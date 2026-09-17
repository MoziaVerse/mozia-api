package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetMoziaConsumptionReport(c *gin.Context) {
	start, startErr := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	end, endErr := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	if startErr != nil || endErr != nil || start <= 0 || end <= start || end-start > 31*86400 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请指定有效的统计起止时间（最多 31 天，结束时间不包含在内）"})
		return
	}
	rows, err := model.GetMoziaConsumptionReport(start, end)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"start_timestamp": start, "end_timestamp": end, "rows": rows})
}
