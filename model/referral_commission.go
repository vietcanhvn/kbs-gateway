package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// ReferralCommission là một khoản hoa hồng giới thiệu: người được mời (InviteeId)
// nạp tiền thật thành công thì người đã mời họ (InviterId) nhận Percent% của
// hạn mức vừa nạp. Mỗi đơn nạp tối đa một khoản (TopUpId là duy nhất).
//
// Hoa hồng cộng vào aff_quota / aff_history của người mời - cùng chỗ với thưởng
// mời đăng ký - nên người mời tự chuyển sang số dư ở trang Ví như trước.
type ReferralCommission struct {
	Id         int     `json:"id"`
	InviterId  int     `json:"inviter_id" gorm:"index"`
	InviteeId  int     `json:"invitee_id" gorm:"index"`
	TopUpId    int     `json:"top_up_id" gorm:"uniqueIndex"`
	TradeNo    string  `json:"trade_no" gorm:"type:varchar(255)"`
	TopUpQuota int     `json:"top_up_quota"`
	Percent    float64 `json:"percent"`
	Quota      int     `json:"quota"`
	// SeenTime = 0: người mời chưa xem khoản này (giao diện hiện lời chúc mừng).
	SeenTime    int64 `json:"seen_time" gorm:"bigint"`
	CreatedTime int64 `json:"created_time" gorm:"bigint;index"`
}

// grantReferralCommissionTx ghi hoa hồng cho người mời BÊN TRONG giao dịch hoàn
// tất đơn nạp, ngay sau khi hạn mức đã cộng cho người nạp. creditedQuota là hạn
// mức thực cộng (mỗi cổng tính Amount/Money một kiểu, chỉ số này cùng nghĩa).
//
// Chạy trong một điểm lưu (savepoint): hoa hồng lỗi thì chỉ hoa hồng bị huỷ,
// đơn nạp của khách vẫn hoàn tất. Không bao giờ trả lỗi ra ngoài.
func grantReferralCommissionTx(tx *gorm.DB, topUp *TopUp, creditedQuota int) *ReferralCommission {
	percent := common.ReferralCommissionPercent
	if percent <= 0 || creditedQuota <= 0 || !operation_setting.IsPaymentComplianceConfirmed() {
		return nil
	}

	var commission *ReferralCommission
	err := tx.Transaction(func(sp *gorm.DB) error {
		var invitee User
		if err := sp.Select("id", "inviter_id").Where("id = ?", topUp.UserId).First(&invitee).Error; err != nil {
			return err
		}
		if invitee.InviterId == 0 || invitee.InviterId == invitee.Id {
			return nil
		}
		// Làm tròn XUỐNG: không bao giờ trả hoa hồng nhiều hơn tỷ lệ đã đặt.
		quota := common.QuotaFromDecimal(
			decimal.NewFromInt(int64(creditedQuota)).Mul(decimal.NewFromFloat(percent)).Div(decimal.NewFromInt(100)).Floor(),
		)
		if quota <= 0 {
			return nil
		}
		row := &ReferralCommission{
			InviterId:   invitee.InviterId,
			InviteeId:   invitee.Id,
			TopUpId:     topUp.Id,
			TradeNo:     topUp.TradeNo,
			TopUpQuota:  creditedQuota,
			Percent:     percent,
			Quota:       quota,
			CreatedTime: common.GetTimestamp(),
		}
		if err := sp.Create(row).Error; err != nil {
			return err
		}
		result := sp.Model(&User{}).Where("id = ?", invitee.InviterId).Updates(map[string]interface{}{
			"aff_quota":   gorm.Expr("aff_quota + ?", quota),
			"aff_history": gorm.Expr("aff_history + ?", quota),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			// Người mời đã bị xoá: không có ai để trả, bỏ cả dòng hoa hồng.
			return gorm.ErrRecordNotFound
		}
		commission = row
		return nil
	})
	if err != nil {
		common.SysError(fmt.Sprintf("referral commission skipped trade_no=%s user_id=%d: %s", topUp.TradeNo, topUp.UserId, err.Error()))
		return nil
	}
	return commission
}

// recordReferralCommissionLog ghi nhật ký cho người mời SAU khi giao dịch đã
// commit (RecordLog dùng kết nối riêng, không gọi được trong giao dịch).
func recordReferralCommissionLog(commission *ReferralCommission) {
	if commission == nil {
		return
	}
	RecordLog(commission.InviterId, LogTypeSystem, fmt.Sprintf(
		"Hoa hồng giới thiệu: %s (%s%% của lượt nạp %s từ người bạn đã mời)",
		logger.LogQuota(commission.Quota),
		decimal.NewFromFloat(commission.Percent).String(),
		logger.LogQuota(commission.TopUpQuota),
	))
}

// ReferralCommissionSummary là các con số tổng. InviterId = 0 khi tính toàn hệ thống.
type ReferralCommissionSummary struct {
	TotalQuota      int64 `json:"total_quota"`
	TotalCount      int64 `json:"total_count"`
	TotalTopUpQuota int64 `json:"total_top_up_quota"`
	PayingInvitees  int64 `json:"paying_invitees"`
	InviterCount    int64 `json:"inviter_count"`
	UnseenQuota     int64 `json:"unseen_quota"`
	UnseenCount     int64 `json:"unseen_count"`
}

func referralCommissionScope(inviterId int) *gorm.DB {
	query := DB.Model(&ReferralCommission{})
	if inviterId > 0 {
		query = query.Where("inviter_id = ?", inviterId)
	}
	return query
}

// GetReferralCommissionSummary tính tổng cho một người mời (inviterId > 0) hoặc
// toàn hệ thống (inviterId = 0).
func GetReferralCommissionSummary(inviterId int) (*ReferralCommissionSummary, error) {
	summary := &ReferralCommissionSummary{}
	err := referralCommissionScope(inviterId).
		Select("COALESCE(SUM(quota), 0) AS total_quota, COUNT(*) AS total_count, COALESCE(SUM(top_up_quota), 0) AS total_top_up_quota, COUNT(DISTINCT invitee_id) AS paying_invitees, COUNT(DISTINCT inviter_id) AS inviter_count").
		Scan(summary).Error
	if err != nil {
		return nil, err
	}
	if inviterId > 0 {
		unseen := struct {
			UnseenQuota int64
			UnseenCount int64
		}{}
		err = referralCommissionScope(inviterId).Where("seen_time = ?", 0).
			Select("COALESCE(SUM(quota), 0) AS unseen_quota, COUNT(*) AS unseen_count").
			Scan(&unseen).Error
		if err != nil {
			return nil, err
		}
		summary.UnseenQuota = unseen.UnseenQuota
		summary.UnseenCount = unseen.UnseenCount
	}
	return summary, nil
}

// GetReferralCommissions trả một trang hoa hồng, mới nhất trước.
func GetReferralCommissions(inviterId int, pageInfo *common.PageInfo) (items []*ReferralCommission, total int64, err error) {
	if err = referralCommissionScope(inviterId).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = referralCommissionScope(inviterId).Order("id desc").
		Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&items).Error
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// ReferralCommissionPoint là một điểm cho biểu đồ theo thời gian. Trình duyệt tự
// gom theo ngày của múi giờ người xem.
type ReferralCommissionPoint struct {
	CreatedTime int64 `json:"created_time"`
	Quota       int   `json:"quota"`
}

// GetRecentReferralCommissionPoints lấy các khoản từ mốc since, tối đa limit dòng mới nhất.
func GetRecentReferralCommissionPoints(inviterId int, since int64, limit int) (points []*ReferralCommissionPoint, err error) {
	err = referralCommissionScope(inviterId).Where("created_time >= ?", since).
		Select("created_time", "quota").Order("id desc").Limit(limit).Find(&points).Error
	return points, err
}

// MarkReferralCommissionsSeen đánh dấu người mời đã xem mọi khoản mới.
func MarkReferralCommissionsSeen(inviterId int) error {
	return DB.Model(&ReferralCommission{}).
		Where("inviter_id = ? AND seen_time = ?", inviterId, 0).
		Update("seen_time", common.GetTimestamp()).Error
}

// TopReferrer là một dòng của bảng xếp hạng người giới thiệu.
type TopReferrer struct {
	InviterId    int   `json:"inviter_id"`
	TotalQuota   int64 `json:"total_quota"`
	TotalCount   int64 `json:"total_count"`
	InviteeCount int64 `json:"invitee_count"`
}

// GetTopReferrers xếp hạng người giới thiệu theo tổng hoa hồng đã nhận.
func GetTopReferrers(limit int) (rows []*TopReferrer, err error) {
	err = DB.Model(&ReferralCommission{}).
		Select("inviter_id, COALESCE(SUM(quota), 0) AS total_quota, COUNT(*) AS total_count, COUNT(DISTINCT invitee_id) AS invitee_count").
		Group("inviter_id").Order("total_quota DESC").Limit(limit).Scan(&rows).Error
	return rows, err
}

// CountReferredUsers đếm số tài khoản đăng ký qua liên kết giới thiệu.
func CountReferredUsers() (total int64, err error) {
	err = DB.Model(&User{}).Where("inviter_id > ?", 0).Count(&total).Error
	return total, err
}

// GetUsernamesByIds trả tên đăng nhập theo mã người dùng, kể cả tài khoản đã xoá
// (bảng thống kê vẫn phải hiện được tên của các khoản cũ).
func GetUsernamesByIds(ids []int) (map[int]string, error) {
	names := make(map[int]string, len(ids))
	if len(ids) == 0 {
		return names, nil
	}
	var users []User
	if err := DB.Unscoped().Select("id", "username").Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil, err
	}
	for _, user := range users {
		names[user.Id] = user.Username
	}
	return names, nil
}
