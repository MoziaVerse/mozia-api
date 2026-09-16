package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAdminUserTokenGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldDB, oldLogDB, oldRedis, oldMaster := model.DB, model.LOG_DB, common.RedisEnabled, common.IsMasterNode
	oldRatios, oldUsable := ratio_setting.GroupRatio2JSONString(), setting.UserUsableGroups2JSONString()
	db, err := gorm.Open(sqlite.Open("file:admin-token-group?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Log{}, &model.CasbinRule{}))
	model.DB, model.LOG_DB, common.RedisEnabled, common.IsMasterNode = db, db, false, false
	require.NoError(t, authz.Init(db))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"private":1,"other_private":1}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","auto":"Auto"}`))
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.RedisEnabled, common.IsMasterNode = oldDB, oldLogDB, oldRedis, oldMaster
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldRatios))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(oldUsable))
		sqlDB, closeErr := db.DB()
		require.NoError(t, closeErr)
		require.NoError(t, sqlDB.Close())
	})
	for _, user := range []model.User{
		{Id: 1, Username: "root", Role: common.RoleRootUser, Status: common.UserStatusEnabled},
		{Id: 2, Username: "operator", Role: common.RoleAdminUser, Status: common.UserStatusEnabled},
		{Id: 3, Username: "customer", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "private"},
		{Id: 4, Username: "other", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default"},
	} {
		user.SetAccessToken(fmt.Sprintf("test-access-%d", user.Id))
		user.AffCode = fmt.Sprintf("test-aff-%d", user.Id)
		require.NoError(t, db.Create(&user).Error)
	}
	require.NoError(t, authz.SetUserPermissions(2, authz.PermissionsMap{"user_management": {"read": true, "group_write": false}}))
	ips := "192.0.2.1"
	token := model.Token{Id: 10, UserId: 3, Name: "existing-key", Key: "secret-must-never-be-returned", Group: "default", Status: 1, RemainQuota: 900, UsedQuota: 100, ModelLimitsEnabled: true, ModelLimits: "test-model", AllowIps: &ips, CrossGroupRetry: true}
	require.NoError(t, db.Create(&token).Error)
	require.NoError(t, db.Create(&model.Token{Id: 11, UserId: 2, Name: "admin-key", Key: "admin-secret", Group: "default"}).Error)
	engine := gin.New()
	engine.Use(sessions.Sessions("test", cookie.NewStore([]byte("test-session-secret"))))
	SetApiRouter(engine)
	request := func(method, path, body string, actor int) (*httptest.ResponseRecorder, map[string]any) {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if actor != 0 {
			r.Header.Set("Authorization", fmt.Sprintf("Bearer test-access-%d", actor))
			r.Header.Set("New-Api-User", fmt.Sprint(actor))
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		var response map[string]any
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
		assert.NotContains(t, w.Body.String(), token.Key)
		return w, response
	}
	path := "/api/user/3/tokens/10/group"
	_, response := request(http.MethodGet, path, "", 2)
	require.Equal(t, true, response["success"])
	assert.Equal(t, map[string]any{"id": float64(10), "user_id": float64(3), "name": "existing-key", "group": "default", "status": float64(1)}, response["data"])
	for _, tc := range []struct {
		name, path, body string
		actor            int
	}{
		{"unauthenticated", path, `{"group":"private","expected_group":"default"}`, 0},
		{"ordinary user", path, `{"group":"private","expected_group":"default"}`, 3},
		{"read-only admin", path, `{"group":"private","expected_group":"default"}`, 2},
		{"wrong owner", "/api/user/4/tokens/10/group", `{"group":"default","expected_group":"default"}`, 1},
		{"missing precondition", path, `{"group":"private"}`, 1},
		{"stale precondition", path, `{"group":"private","expected_group":"auto"}`, 1},
		{"unavailable group", path, `{"group":"other_private","expected_group":"default"}`, 1},
		{"unknown group", path, `{"group":"missing","expected_group":"default"}`, 1},
		{"empty group", path, `{"group":"","expected_group":"default"}`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, response := request(http.MethodPut, tc.path, tc.body, tc.actor)
			assert.Equal(t, false, response["success"])
			var unchanged model.Token
			require.NoError(t, db.First(&unchanged, token.Id).Error)
			assert.Equal(t, token, unchanged)
		})
	}
	require.NoError(t, authz.SetUserPermissions(2, authz.PermissionsMap{"user_management": {"read": true, "group_write": true}}))
	_, response = request(http.MethodPut, "/api/user/2/tokens/11/group", `{"group":"default","expected_group":"default"}`, 2)
	assert.Equal(t, false, response["success"], "admin cannot manage a peer admin")
	_, response = request(http.MethodPut, path, `{"group":"private","expected_group":"default","key":"replacement","remain_quota":0,"status":2}`, 2)
	require.Equal(t, true, response["success"])
	assert.Equal(t, true, response["data"].(map[string]any)["cache_invalidated"])
	var updated model.Token
	require.NoError(t, db.First(&updated, token.Id).Error)
	expected := token
	expected.Group = "private"
	assert.Equal(t, expected, updated, "only the group may change")
	var logs []model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeManage).Find(&logs).Error)
	var audit map[string]any
	for _, entry := range logs {
		var other map[string]any
		require.NoError(t, common.UnmarshalJsonStr(entry.Other, &other))
		if op, ok := other["op"].(map[string]any); ok && op["action"] == "token.group_update" {
			audit = other
		}
	}
	require.NotNil(t, audit)
	assert.Equal(t, float64(2), audit["admin_info"].(map[string]any)["admin_id"])
	assert.Equal(t, "access_token", audit["admin_info"].(map[string]any)["auth_method"])
	assert.Equal(t, float64(3), audit["op"].(map[string]any)["params"].(map[string]any)["target_user_id"])
	_, response = request(http.MethodGet, path, "", 1)
	assert.Equal(t, "private", response["data"].(map[string]any)["group"])

	// A group update using a previously loaded token must not restore consumed quota.
	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", token.Id).Updates(map[string]any{"remain_quota": 800, "used_quota": 200}).Error)
	invalidated, err := updated.UpdateGroup("default")
	require.NoError(t, err)
	assert.True(t, invalidated)
	require.NoError(t, db.First(&updated, token.Id).Error)
	assert.Equal(t, 800, updated.RemainQuota)
	assert.Equal(t, 200, updated.UsedQuota)
	_, err = expected.UpdateGroup("other_private")
	assert.Error(t, err, "concurrent group change must be rejected")
}
