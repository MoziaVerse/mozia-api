package model

import (
	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// VendorFiling 备案信息表，通过 VendorId 关联 Vendor。
// 由于新增字段可能与上游 NewAPI 代码冲突，因此采用独立表存储备案信息。
type VendorFiling struct {
	Id           int            `json:"id"`
	VendorId     int            `json:"vendor_id" gorm:"not null;uniqueIndex:uk_vendor_filing_vendor_id_delete_at,priority:1"`
	FilingTime   int64          `json:"filing_time" gorm:"bigint"`
	Location     string         `json:"location" gorm:"size:128"`
	FilingEntity string         `json:"filing_entity" gorm:"size:256"`
	FilingNumber string         `json:"filing_number" gorm:"size:128"`
	CreatedTime  int64          `json:"created_time" gorm:"bigint"`
	UpdatedTime  int64          `json:"updated_time" gorm:"bigint"`
	DeletedAt    gorm.DeletedAt `json:"-" gorm:"index;uniqueIndex:uk_vendor_filing_vendor_id_delete_at,priority:2"`
}

// VendorFilingWithVendor 返回给管理平台的视图，附带 Vendor 基本信息。
type VendorFilingWithVendor struct {
	VendorFiling
	VendorName        string `json:"vendor_name"`
	VendorDescription string `json:"vendor_description"`
	VendorIcon        string `json:"vendor_icon"`
	VendorStatus      int    `json:"vendor_status"`
}

func (f *VendorFiling) Insert() error {
	now := common.GetTimestamp()
	f.CreatedTime = now
	f.UpdatedTime = now
	return DB.Create(f).Error
}

func (f *VendorFiling) Update() error {
	f.UpdatedTime = common.GetTimestamp()
	return DB.Save(f).Error
}

func (f *VendorFiling) Delete() error {
	return DB.Delete(f).Error
}

func GetVendorFilingByID(id int) (*VendorFilingWithVendor, error) {
	var f VendorFiling
	if err := DB.First(&f, id).Error; err != nil {
		return nil, err
	}
	result := &VendorFilingWithVendor{VendorFiling: f}
	var v Vendor
	if err := DB.First(&v, f.VendorId).Error; err == nil {
		result.VendorName = v.Name
		result.VendorDescription = v.Description
		result.VendorIcon = v.Icon
		result.VendorStatus = v.Status
	}
	return result, nil
}

// IsVendorFilingDuplicated 同一 VendorId 只能存在一条备案。
func IsVendorFilingDuplicated(id int, vendorId int) (bool, error) {
	if vendorId == 0 {
		return false, nil
	}
	var cnt int64
	err := DB.Model(&VendorFiling{}).Where("vendor_id = ? AND id <> ?", vendorId, id).Count(&cnt).Error
	return cnt > 0, err
}

// GetAllVendorFilings 分页获取备案信息，并附带 Vendor 基本信息。
func GetAllVendorFilings(keyword string, offset int, limit int) ([]*VendorFilingWithVendor, int64, error) {
	db := DB.Model(&VendorFiling{})
	if keyword != "" {
		like := "%" + keyword + "%"
		var vendorIDs []int
		if err := DB.Model(&Vendor{}).Where("name LIKE ? OR description LIKE ?", like, like).Pluck("id", &vendorIDs).Error; err != nil {
			return nil, 0, err
		}
		if len(vendorIDs) > 0 {
			db = db.Where("location LIKE ? OR filing_entity LIKE ? OR filing_number LIKE ? OR vendor_id IN ?", like, like, like, vendorIDs)
		} else {
			db = db.Where("location LIKE ? OR filing_entity LIKE ? OR filing_number LIKE ?", like, like, like)
		}
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var filings []VendorFiling
	if err := db.Offset(offset).Limit(limit).Order("id DESC").Find(&filings).Error; err != nil {
		return nil, 0, err
	}
	if len(filings) == 0 {
		return []*VendorFilingWithVendor{}, total, nil
	}

	vendorIDs := make([]int, 0, len(filings))
	for _, f := range filings {
		vendorIDs = append(vendorIDs, f.VendorId)
	}
	var vendors []Vendor
	if err := DB.Where("id IN ?", vendorIDs).Find(&vendors).Error; err != nil {
		return nil, 0, err
	}
	vendorMap := make(map[int]Vendor, len(vendors))
	for _, v := range vendors {
		vendorMap[v.Id] = v
	}

	result := make([]*VendorFilingWithVendor, 0, len(filings))
	for _, f := range filings {
		item := &VendorFilingWithVendor{VendorFiling: f}
		if v, ok := vendorMap[f.VendorId]; ok {
			item.VendorName = v.Name
			item.VendorDescription = v.Description
			item.VendorIcon = v.Icon
			item.VendorStatus = v.Status
		}
		result = append(result, item)
	}
	return result, total, nil
}
