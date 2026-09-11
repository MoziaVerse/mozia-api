package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func supplierResourceFixture(t *testing.T) *gorm.DB {
	t.Helper()
	client, _ := supplierRedisFixture(t)
	oldRedis, oldEnabled, oldCache := common.RDB, common.RedisEnabled, common.MemoryCacheEnabled
	common.OptionMapRWMutex.Lock()
	oldOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	common.RDB = client
	common.RedisEnabled = true
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.RDB = oldRedis
		common.RedisEnabled = oldEnabled
		common.MemoryCacheEnabled = oldCache
		common.OptionMapRWMutex.Lock()
		common.OptionMap = oldOptions
		common.OptionMapRWMutex.Unlock()
	})
	require.NoError(t, client.Del(context.Background(), supplierRuntimeKey).Err())
	db := setupMaterialServiceTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.Supplier{}, &model.SupplierPool{}, &model.SupplierBinding{}, &model.SupplierRoutingRule{}, &model.RoutingRevision{}, &model.SupplierAttempt{}, &model.SystemTask{}, &model.SystemTaskLock{}))
	require.NoError(t, model.MigrateSupplierResources())
	require.NoError(t, model.RegisterSupplierChannelGuards(db))
	return db
}

func createSupplierResourceForTest(t *testing.T, kind, key, body string, owner int64) *SupplierResourceResult {
	t.Helper()
	result, err := MutateSupplierResource(context.Background(), SupplierResourceMutation{Kind: kind, Action: "create", UserID: 7, Scope: fmt.Sprintf("%s/%d", kind, owner), IdempotencyKey: key, Patch: []byte(body), SupplierID: owner})
	require.NoError(t, err)
	return result
}

func TestSupplierResourceIndependentSavesAndIdempotency(t *testing.T) {
	db := supplierResourceFixture(t)
	a := createSupplierResourceForTest(t, "supplier", "a", `{"name":"A","enabled":false}`, 0)
	b := createSupplierResourceForTest(t, "supplier", "b", `{"name":"B","enabled":true}`, 0)
	assert.Equal(t, "applied", a.Application)
	first := a.Resource.(*model.Supplier)
	second := b.Resource.(*model.Supplier)
	assert.NotEqual(t, first.ID, second.ID)
	retry := createSupplierResourceForTest(t, "supplier", "a", `{"enabled":false,"name":"A"}`, 0)
	assert.True(t, retry.Replayed)
	assert.Equal(t, first.ID, retry.Resource.(*model.Supplier).ID)
	_, err := MutateSupplierResource(context.Background(), SupplierResourceMutation{Kind: "supplier", Action: "create", UserID: 7, Scope: "supplier/0", IdempotencyKey: "a", Patch: []byte(`{"name":"different"}`)})
	var problem *model.SupplierResourceError
	require.ErrorAs(t, err, &problem)
	assert.Equal(t, "idempotency_key_conflict", problem.Code)
	common.RedisEnabled = false
	request := SupplierResourceMutation{Kind: "supplier", ID: fmt.Sprint(first.ID), Action: "patch", IfMatch: SupplierResourceETag("supplier", first), Patch: []byte(`{"name":"A2","contact":""}`)}
	result, err := MutateSupplierResource(context.Background(), request)
	require.NoError(t, err)
	assert.Equal(t, "not_required", result.Application)
	changed := result.Resource.(*model.Supplier)
	assert.Equal(t, int64(2), changed.Version)
	assert.False(t, changed.Enabled)
	_, err = MutateSupplierResource(context.Background(), request)
	require.ErrorAs(t, err, &problem)
	assert.Equal(t, 412, problem.Status)
	request.IfMatch = ""
	_, err = MutateSupplierResource(context.Background(), request)
	require.ErrorAs(t, err, &problem)
	assert.Equal(t, 428, problem.Status)
	var unchanged model.Supplier
	require.NoError(t, db.First(&unchanged, second.ID).Error)
	assert.Equal(t, "B", unchanged.Name)
	assert.Equal(t, int64(1), unchanged.Version)
}

func TestSupplierResourcePatchAndBindingContracts(t *testing.T) {
	db := supplierResourceFixture(t)
	supplier := createSupplierResourceForTest(t, "supplier", "a", `{"name":"A"}`, 0).Resource.(*model.Supplier)
	pool := createSupplierResourceForTest(t, "pool", "p", `{"name":"P","failure_domain":"dc-a","limits":{"concurrency":10,"rpm":100,"tpm":10000},"max_execution_seconds":60,"input_safety_percent":110,"models":[{"name":"test","version":"v1","context_tokens":1000,"max_output_tokens":100}]}`, supplier.ID).Resource.(*model.SupplierPool)
	request := SupplierResourceMutation{Kind: "pool", ID: fmt.Sprint(pool.ID), Action: "patch", IfMatch: SupplierResourceETag("pool", pool), Patch: []byte(`{"limits":{"rpm":150},"enabled":false}`)}
	result, err := MutateSupplierResource(context.Background(), request)
	require.NoError(t, err)
	pool = result.Resource.(*model.SupplierPool)
	assert.Equal(t, int64(150), pool.Limits.RPM)
	assert.Equal(t, int64(10), pool.Limits.Concurrency)
	require.Len(t, pool.Models, 1)
	for _, body := range []string{`{"limits":{"rpm":0}}`, `{"supplier_id":999}`, `{"models":[{"name":"test","version":""}]}`, `{"limits":{"rpmm":2}}`, `{"enabled":null}`, `{"version":123}`} {
		request.IfMatch = SupplierResourceETag("pool", pool)
		request.Patch = []byte(body)
		_, err := MutateSupplierResource(context.Background(), request)
		var problem *model.SupplierResourceError
		require.ErrorAs(t, err, &problem)
		assert.Equal(t, 422, problem.Status)
		assert.NotEmpty(t, problem.FieldErrors)
	}
	channel := model.Channel{Name: "C", Models: "test,other", Type: constant.ChannelTypeOpenAI, OtherSettings: `{"disable_store":true}`}
	require.NoError(t, db.Create(&channel).Error)
	bindingBody := fmt.Sprintf(`{"channel_id":%d,"pool_id":%d,"model":"test"}`, channel.Id, pool.ID)
	binding := createSupplierResourceForTest(t, "binding", "b", bindingBody, 0).Resource.(*model.SupplierBinding)
	require.NoError(t, db.First(&channel, channel.Id).Error)
	assert.Equal(t, supplier.ID, channel.SupplierID)
	assert.True(t, channel.GetOtherSettings().DisableStore)
	assert.Error(t, db.Model(&channel).Update("models", "other").Error)
	assert.Error(t, db.Where("id = ?", channel.Id).Delete(&model.Channel{}).Error)
	assert.Error(t, db.Model(&model.Channel{}).Where("id = ?", channel.Id).Updates(map[string]any{"settings": "{}"}).Error)
	assert.Error(t, db.Model(&model.Channel{}).Where("id = ?", channel.Id).Updates(model.Channel{Type: 2}).Error)
	require.NoError(t, db.Model(&channel).Update("name", "C2").Error)
	_, err = MutateSupplierResource(context.Background(), SupplierResourceMutation{Kind: "pool", ID: fmt.Sprint(pool.ID), Action: "delete", IfMatch: SupplierResourceETag("pool", pool)})
	var problem *model.SupplierResourceError
	require.ErrorAs(t, err, &problem)
	assert.Equal(t, "resource_in_use", problem.Code)
	attempt := model.SupplierAttempt{RequestID: "pending", Attempt: 1, Status: "unknown"}
	require.NoError(t, db.Create(&attempt).Error)
	deletion := SupplierResourceMutation{Kind: "binding", ID: fmt.Sprint(binding.ID), Action: "delete", IfMatch: SupplierResourceETag("binding", binding)}
	_, err = MutateSupplierResource(context.Background(), deletion)
	require.ErrorAs(t, err, &problem)
	assert.Equal(t, "binding_in_use", problem.Code)
	require.NoError(t, db.Model(&attempt).Update("status", "cancelled").Error)
	_, err = MutateSupplierResource(context.Background(), deletion)
	require.NoError(t, err)
	replacement := createSupplierResourceForTest(t, "binding", "replacement", bindingBody, 0).Resource.(*model.SupplierBinding)
	assert.NotEqual(t, binding.ID, replacement.ID)
	_, err = MutateSupplierResource(context.Background(), SupplierResourceMutation{Kind: "binding", Action: "create", UserID: 7, Scope: "binding/0", IdempotencyKey: "b", Patch: []byte(bindingBody)})
	require.ErrorAs(t, err, &problem)
	assert.Equal(t, "resource_deleted", problem.Code)
}

type supplierApplyResponseLoss struct{ armed bool }

func (h *supplierApplyResponseLoss) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	return ctx, nil
}
func (h *supplierApplyResponseLoss) AfterProcess(ctx context.Context, cmd redis.Cmder) error {
	args := cmd.Args()
	if h.armed && cmd.Err() == nil && len(args) > 1 && (fmt.Sprint(args[1]) == supplierLeaseApply.Hash() || strings.Contains(fmt.Sprint(args[1]), "newer revision already applied")) {
		h.armed = false
		return errors.New("simulated lost apply acknowledgement")
	}
	return nil
}
func (h *supplierApplyResponseLoss) BeforeProcessPipeline(ctx context.Context, cmd []redis.Cmder) (context.Context, error) {
	return ctx, nil
}
func (h *supplierApplyResponseLoss) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}

func TestSupplierResourcePendingApplyRecoveryAndFencing(t *testing.T) {
	db := supplierResourceFixture(t)
	loss := &supplierApplyResponseLoss{armed: true}
	common.RDB.AddHook(loss)
	result := createSupplierResourceForTest(t, "supplier", "a", `{"name":"A"}`, 0)
	assert.Equal(t, "pending", result.Application)
	assert.Equal(t, 202, SupplierMutationHTTPStatus(result, "create"))
	supplier := result.Resource.(*model.Supplier)
	var state model.SupplierResourceState
	require.NoError(t, model.ReadSupplierOption(db, model.SupplierResourceStateKey, &state))
	assert.Greater(t, state.TargetRevision, state.AppliedRevision)
	_, err := MutateSupplierResource(context.Background(), SupplierResourceMutation{Kind: "supplier", Action: "patch", ID: fmt.Sprint(supplier.ID), IfMatch: SupplierResourceETag("supplier", supplier), Patch: []byte(`{"enabled":true}`)})
	var problem *model.SupplierResourceError
	require.ErrorAs(t, err, &problem)
	assert.Equal(t, "configuration_apply_pending", problem.Code)
	metadata, err := MutateSupplierResource(context.Background(), SupplierResourceMutation{Kind: "supplier", Action: "patch", ID: fmt.Sprint(supplier.ID), IfMatch: SupplierResourceETag("supplier", supplier), Patch: []byte(`{"name":"updated during apply"}`)})
	require.NoError(t, err)
	assert.Equal(t, "not_required", metadata.Application)
	ctx := context.Background()
	require.NoError(t, common.RDB.Set(ctx, "supplier-routing:ledger-sentinel", "reservation", 0).Err())
	require.NoError(t, ReconcileSupplierConfiguration(ctx))
	require.NoError(t, model.ReadSupplierOption(db, model.SupplierResourceStateKey, &state))
	assert.Equal(t, state.TargetRevision, state.AppliedRevision)
	assert.Equal(t, "reservation", common.RDB.Get(ctx, "supplier-routing:ledger-sentinel").Val())
	require.NoError(t, common.RDB.Set(ctx, supplierPublishLock, "new-owner", 30*time.Second).Err())
	assert.Error(t, supplierLeaseGate.Run(ctx, common.RDB, []string{supplierPublishLock, supplierRuntimeKey}, "expired-owner").Err())
	assert.Error(t, supplierLeaseApply.Run(ctx, common.RDB, []string{supplierPublishLock, supplierRuntimeKey}, "expired-owner", `{"config":{"revision":0}}`).Err())
	releaseSupplierPublisher("expired-owner")
	assert.Equal(t, "new-owner", common.RDB.Get(ctx, supplierPublishLock).Val())
	require.NoError(t, common.RDB.Del(ctx, supplierPublishLock, supplierRuntimeKey).Err())
	require.NoError(t, db.Create(&model.SupplierAttempt{RequestID: "uncertain", Attempt: 1, Status: "unknown"}).Error)
	assert.ErrorContains(t, ReconcileSupplierConfiguration(ctx), "uncertain")
	assert.Zero(t, common.RDB.Exists(ctx, supplierRuntimeKey).Val())
}

func TestSupplierResourceRestoreKeepsCurrentCapacity(t *testing.T) {
	db := supplierResourceFixture(t)
	supplier := createSupplierResourceForTest(t, "supplier", "a", `{"name":"A"}`, 0).Resource.(*model.Supplier)
	pool := createSupplierResourceForTest(t, "pool", "p", `{"name":"P","failure_domain":"dc-a","limits":{"concurrency":10,"rpm":100,"tpm":10000},"max_execution_seconds":60,"input_safety_percent":110,"models":[{"name":"test","version":"v1","context_tokens":1000,"max_output_tokens":100}]}`, supplier.ID).Resource.(*model.SupplierPool)
	channel := model.Channel{Name: "C", Models: "test", Type: 1}
	require.NoError(t, db.Create(&channel).Error)
	createSupplierResourceForTest(t, "binding", "b", fmt.Sprintf(`{"channel_id":%d,"model":"test","pool_id":%d}`, channel.Id, pool.ID), 0)
	ruleBody := fmt.Sprintf(`{"model":"test","mode":"capacity","targets":[{"supplier_id":%d,"weight":100}],"max_attempts":2,"timeout_seconds":120,"health":{"window_seconds":60,"min_samples":10,"failure_percent":20,"max_ttft_ms":5000,"cooldown_seconds":30,"trial_percent":10}}`, supplier.ID)
	first := createSupplierResourceForTest(t, "rule", "r", ruleBody, 0)
	rule := first.Resource.(*model.SupplierRoutingRule)
	changed, err := MutateSupplierResource(context.Background(), SupplierResourceMutation{Kind: "rule", ID: rule.ID, Action: "patch", IfMatch: SupplierResourceETag("rule", rule), Patch: []byte(`{"mode":"share"}`)})
	require.NoError(t, err)
	rule = changed.Resource.(*model.SupplierRoutingRule)
	_, err = MutateSupplierResource(context.Background(), SupplierResourceMutation{Kind: "pool", ID: fmt.Sprint(pool.ID), Action: "patch", IfMatch: SupplierResourceETag("pool", pool), Patch: []byte(`{"limits":{"rpm":50}}`)})
	require.NoError(t, err)
	restored, err := MutateSupplierResource(context.Background(), SupplierResourceMutation{Kind: "rule", ID: rule.ID, Action: "patch", IfMatch: SupplierResourceETag("rule", rule), RestoreRevision: first.Revision})
	require.NoError(t, err)
	assert.Equal(t, "capacity", restored.Resource.(*model.SupplierRoutingRule).Mode)
	require.NoError(t, db.First(pool, pool.ID).Error)
	assert.Equal(t, int64(50), pool.Limits.RPM)
	var count int64
	require.NoError(t, db.Model(&model.SupplierBinding{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}
