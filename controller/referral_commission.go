package controller

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// Hoa hồng giới thiệu theo tiền nạp: người mời xem phần của mình, quản trị xem
// toàn hệ thống. Việc cộng hoa hồng nằm ở model/referral_commission.go.

const (
	// Biểu đồ lấy 90 ngày gần nhất, tối đa 5000 khoản mới nhất.
	referralCommissionChartDays      = 90
	referralCommissionChartMaxPoints = 5000
	referralCommissionTopLimit       = 20
)

type referralCommissionItem struct {
	Id          int     `json:"id"`
	CreatedTime int64   `json:"created_time"`
	InviterId   int     `json:"inviter_id,omitempty"`
	InviterName string  `json:"inviter_name,omitempty"`
	InviteeId   int     `json:"invitee_id,omitempty"`
	InviteeName string  `json:"invitee_name"`
	TradeNo     string  `json:"trade_no,omitempty"`
	TopUpQuota  int     `json:"top_up_quota"`
	Percent     float64 `json:"percent"`
	Quota       int     `json:"quota"`
}

// maskUsername che tên người được mời với người mời: "nguyenvana" -> "ng***".
func maskUsername(name string) string {
	runes := []rune(name)
	if len(runes) <= 2 {
		return string(runes) + "***"
	}
	return string(runes[:2]) + "***"
}

func referralCommissionChartSince() int64 {
	return time.Now().AddDate(0, 0, -referralCommissionChartDays).Unix()
}

// GetSelfReferralCommissions: danh sách hoa hồng của chính người đang đăng nhập.
func GetSelfReferralCommissions(c *gin.Context) {
	userId := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)
	rows, total, err := model.GetReferralCommissions(userId, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	inviteeIds := make([]int, 0, len(rows))
	for _, row := range rows {
		inviteeIds = append(inviteeIds, row.InviteeId)
	}
	names, err := model.GetUsernamesByIds(inviteeIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]referralCommissionItem, 0, len(rows))
	for _, row := range rows {
		// Người mời chỉ thấy tên đã che, không thấy mã người dùng hay mã đơn.
		items = append(items, referralCommissionItem{
			Id:          row.Id,
			CreatedTime: row.CreatedTime,
			InviteeName: maskUsername(names[row.InviteeId]),
			TopUpQuota:  row.TopUpQuota,
			Percent:     row.Percent,
			Quota:       row.Quota,
		})
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

// GetSelfReferralStats: các con số tổng + dữ liệu biểu đồ của người đang đăng nhập.
func GetSelfReferralStats(c *gin.Context) {
	userId := c.GetInt("id")
	summary, err := model.GetReferralCommissionSummary(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	points, err := model.GetRecentReferralCommissionPoints(userId, referralCommissionChartSince(), referralCommissionChartMaxPoints)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"percent": common.ReferralCommissionPercent,
		"summary": summary,
		"recent":  points,
	})
}

// MarkSelfReferralCommissionsSeen: người mời đã xem lời chúc mừng.
func MarkSelfReferralCommissionsSeen(c *gin.Context) {
	if err := model.MarkReferralCommissionsSeen(c.GetInt("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// AdminGetReferralCommissions: mọi khoản hoa hồng của hệ thống.
func AdminGetReferralCommissions(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	rows, total, err := model.GetReferralCommissions(0, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	userIds := make([]int, 0, len(rows)*2)
	for _, row := range rows {
		userIds = append(userIds, row.InviterId, row.InviteeId)
	}
	names, err := model.GetUsernamesByIds(userIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]referralCommissionItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, referralCommissionItem{
			Id:          row.Id,
			CreatedTime: row.CreatedTime,
			InviterId:   row.InviterId,
			InviterName: names[row.InviterId],
			InviteeId:   row.InviteeId,
			InviteeName: names[row.InviteeId],
			TradeNo:     row.TradeNo,
			TopUpQuota:  row.TopUpQuota,
			Percent:     row.Percent,
			Quota:       row.Quota,
		})
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

type topReferrerItem struct {
	model.TopReferrer
	Username string `json:"username"`
}

// AdminGetReferralStats: tổng hoa hồng, bảng xếp hạng người giới thiệu, dữ liệu biểu đồ.
func AdminGetReferralStats(c *gin.Context) {
	summary, err := model.GetReferralCommissionSummary(0)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	referredUsers, err := model.CountReferredUsers()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	top, err := model.GetTopReferrers(referralCommissionTopLimit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	inviterIds := make([]int, 0, len(top))
	for _, row := range top {
		inviterIds = append(inviterIds, row.InviterId)
	}
	names, err := model.GetUsernamesByIds(inviterIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	topItems := make([]topReferrerItem, 0, len(top))
	for _, row := range top {
		topItems = append(topItems, topReferrerItem{TopReferrer: *row, Username: names[row.InviterId]})
	}
	points, err := model.GetRecentReferralCommissionPoints(0, referralCommissionChartSince(), referralCommissionChartMaxPoints)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"percent":        common.ReferralCommissionPercent,
		"summary":        summary,
		"referred_users": referredUsers,
		"top":            topItems,
		"recent":         points,
	})
}
