package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// GetAllVendorFilings 获取备案信息列表（分页），支持关键字搜索。
func GetAllVendorFilings(c *gin.Context) {
	keyword := c.Query("keyword")
	pageInfo := common.GetPageQuery(c)
	filings, total, err := model.GetAllVendorFilings(keyword, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(filings)
	common.ApiSuccess(c, pageInfo)
}

// GetVendorFiling 根据 ID 获取备案信息。
func GetVendorFiling(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	f, err := model.GetVendorFilingByID(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, f)
}

// CreateVendorFiling 新建备案信息。
func CreateVendorFiling(c *gin.Context) {
	var f model.VendorFiling
	if err := c.ShouldBindJSON(&f); err != nil {
		common.ApiError(c, err)
		return
	}
	if f.VendorId == 0 {
		common.ApiErrorMsg(c, "缺少 vendor_id")
		return
	}
	if _, err := model.GetVendorByID(f.VendorId); err != nil {
		common.ApiErrorMsg(c, "关联的供应商不存在")
		return
	}
	if dup, err := model.IsVendorFilingDuplicated(0, f.VendorId); err != nil {
		common.ApiError(c, err)
		return
	} else if dup {
		common.ApiErrorMsg(c, "该供应商已存在备案信息")
		return
	}
	f.Id = 0
	if err := f.Insert(); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, &f)
}

// UpdateVendorFiling 更新备案信息。
func UpdateVendorFiling(c *gin.Context) {
	var f model.VendorFiling
	if err := c.ShouldBindJSON(&f); err != nil {
		common.ApiError(c, err)
		return
	}
	if f.Id == 0 {
		common.ApiErrorMsg(c, "缺少备案 ID")
		return
	}
	if f.VendorId == 0 {
		common.ApiErrorMsg(c, "缺少 vendor_id")
		return
	}
	if _, err := model.GetVendorByID(f.VendorId); err != nil {
		common.ApiErrorMsg(c, "关联的供应商不存在")
		return
	}
	if dup, err := model.IsVendorFilingDuplicated(f.Id, f.VendorId); err != nil {
		common.ApiError(c, err)
		return
	} else if dup {
		common.ApiErrorMsg(c, "该供应商已存在备案信息")
		return
	}
	if err := f.Update(); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, &f)
}

// DeleteVendorFiling 删除备案信息。
func DeleteVendorFiling(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DB.Delete(&model.VendorFiling{}, id).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
