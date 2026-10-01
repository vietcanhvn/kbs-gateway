package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Bật hoa hồng giới thiệu cho một bài kiểm thử: tỷ lệ, 1 USD = 500000 hạn mức,
// điều khoản thanh toán đã xác nhận.
func enableReferralCommissionForTest(t *testing.T, percent float64) {
	t.Helper()
	truncateTables(t)
	ps := operation_setting.GetPaymentSetting()
	oldConfirmed, oldVersion := ps.ComplianceConfirmed, ps.ComplianceTermsVersion
	oldPercent, oldQuotaPerUnit := common.ReferralCommissionPercent, common.QuotaPerUnit
	ps.ComplianceConfirmed, ps.ComplianceTermsVersion = true, operation_setting.CurrentComplianceTermsVersion
	common.ReferralCommissionPercent = percent
	common.QuotaPerUnit = 500000
	t.Cleanup(func() {
		ps.ComplianceConfirmed, ps.ComplianceTermsVersion = oldConfirmed, oldVersion
		common.ReferralCommissionPercent = oldPercent
		common.QuotaPerUnit = oldQuotaPerUnit
	})
}

func insertReferralUserForTest(t *testing.T, id int, inviterId int) *User {
	t.Helper()
	user := &User{
		Id:        id,
		Username:  fmt.Sprintf("referral_user_%d", id),
		Status:    common.UserStatusEnabled,
		AffCode:   fmt.Sprintf("aff%d", id),
		InviterId: inviterId,
	}
	require.NoError(t, DB.Create(user).Error)
	return user
}

func insertReferralTopUpForTest(t *testing.T, userId int, tradeNo string, provider string, amount int64, money float64) *TopUp {
	t.Helper()
	topUp := &TopUp{
		UserId:          userId,
		Amount:          amount,
		Money:           money,
		TradeNo:         tradeNo,
		PaymentMethod:   provider,
		PaymentProvider: provider,
		CreateTime:      common.GetTimestamp(),
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, DB.Create(topUp).Error)
	return topUp
}

func reloadReferralUserForTest(t *testing.T, id int) *User {
	t.Helper()
	user := &User{}
	require.NoError(t, DB.Unscoped().First(user, id).Error)
	return user
}

func referralCommissionsForTest(t *testing.T) []ReferralCommission {
	t.Helper()
	var rows []ReferralCommission
	require.NoError(t, DB.Order("id asc").Find(&rows).Error)
	return rows
}

func TestPayOSTopUpPaysInviterCommissionExactlyOnce(t *testing.T) {
	enableReferralCommissionForTest(t, 5)
	inviter := insertReferralUserForTest(t, 701, 0)
	invitee := insertReferralUserForTest(t, 702, inviter.Id)
	order := insertReferralTopUpForTest(t, invitee.Id, "REFPAYOS1", PaymentProviderPayOS, 10, 260000)

	alreadyDone, err := RechargePayOS(order.TradeNo, 260000, "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, alreadyDone)

	// Người nạp nhận đủ 10 USD; người mời nhận 5% = 0,5 USD vào phần thưởng chờ chuyển.
	assert.Equal(t, 10*500000, reloadReferralUserForTest(t, invitee.Id).Quota)
	paidInviter := reloadReferralUserForTest(t, inviter.Id)
	assert.Equal(t, 250000, paidInviter.AffQuota)
	assert.Equal(t, 250000, paidInviter.AffHistoryQuota)
	assert.Equal(t, 0, paidInviter.Quota)

	rows := referralCommissionsForTest(t)
	require.Len(t, rows, 1)
	assert.Equal(t, inviter.Id, rows[0].InviterId)
	assert.Equal(t, invitee.Id, rows[0].InviteeId)
	assert.Equal(t, order.Id, rows[0].TopUpId)
	assert.Equal(t, order.TradeNo, rows[0].TradeNo)
	assert.Equal(t, 10*500000, rows[0].TopUpQuota)
	assert.Equal(t, 5.0, rows[0].Percent)
	assert.Equal(t, 250000, rows[0].Quota)
	assert.Zero(t, rows[0].SeenTime)
	assert.NotZero(t, rows[0].CreatedTime)

	// payOS gọi lại lần nữa (webhook lặp, trang quay về): không cộng thêm.
	alreadyDone, err = RechargePayOS(order.TradeNo, 260000, "127.0.0.1")
	require.NoError(t, err)
	assert.True(t, alreadyDone)
	assert.Equal(t, 250000, reloadReferralUserForTest(t, inviter.Id).AffQuota)
	assert.Len(t, referralCommissionsForTest(t), 1)
}

func TestEveryTopUpProviderPaysCommissionOnCreditedQuota(t *testing.T) {
	// Mỗi cổng tính hạn mức được cộng một kiểu (Amount, Money hay hạn mức thô);
	// hoa hồng luôn là 5% của đúng hạn mức đã cộng cho người nạp.
	cases := []struct {
		provider string
		amount   int64
		money    float64
		credited int
		complete func(tradeNo string) error
	}{
		{PaymentProviderEpay, 4, 29.2, 4 * 500000, func(tradeNo string) error {
			_, err := RechargeEpay(tradeNo, "alipay", "127.0.0.1")
			return err
		}},
		{PaymentProviderStripe, 4, 6, 6 * 500000, func(tradeNo string) error {
			return Recharge(tradeNo, "cus_test", "127.0.0.1")
		}},
		{PaymentProviderCreem, 3000000, 6, 3000000, func(tradeNo string) error {
			return RechargeCreem(tradeNo, "", "", "127.0.0.1")
		}},
		{PaymentProviderWaffo, 4, 4, 4 * 500000, func(tradeNo string) error {
			return RechargeWaffo(tradeNo, "127.0.0.1")
		}},
		{PaymentProviderWaffoPancake, 4, 4, 4 * 500000, RechargeWaffoPancake},
		{PaymentProviderPayOS, 4, 104000, 4 * 500000, func(tradeNo string) error {
			_, err := RechargePayOS(tradeNo, 104000, "127.0.0.1")
			return err
		}},
		{"manual:" + PaymentProviderPayOS, 4, 104000, 4 * 500000, func(tradeNo string) error {
			return ManualCompleteTopUp(tradeNo, "127.0.0.1")
		}},
	}
	for index, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			enableReferralCommissionForTest(t, 5)
			inviter := insertReferralUserForTest(t, 800+index*2, 0)
			invitee := insertReferralUserForTest(t, 801+index*2, inviter.Id)
			provider := tc.provider
			if len(provider) > 7 && provider[:7] == "manual:" {
				provider = provider[7:]
			}
			tradeNo := fmt.Sprintf("REFPROVIDER%d", index)
			insertReferralTopUpForTest(t, invitee.Id, tradeNo, provider, tc.amount, tc.money)

			require.NoError(t, tc.complete(tradeNo))

			assert.Equal(t, tc.credited, reloadReferralUserForTest(t, invitee.Id).Quota)
			assert.Equal(t, tc.credited*5/100, reloadReferralUserForTest(t, inviter.Id).AffQuota)
			rows := referralCommissionsForTest(t)
			require.Len(t, rows, 1)
			assert.Equal(t, tc.credited, rows[0].TopUpQuota)
			assert.Equal(t, tc.credited*5/100, rows[0].Quota)
		})
	}
}

func TestReferralCommissionIsSkippedWithoutBlockingTheTopUp(t *testing.T) {
	cases := []struct {
		name    string
		percent float64
		prepare func(t *testing.T, inviter *User, invitee *User)
	}{
		{"tỷ lệ bằng 0 là tắt", 0, func(t *testing.T, inviter *User, invitee *User) {}},
		{"người nạp không do ai giới thiệu", 5, func(t *testing.T, inviter *User, invitee *User) {
			require.NoError(t, DB.Model(&User{}).Where("id = ?", invitee.Id).Update("inviter_id", 0).Error)
		}},
		{"người giới thiệu đã bị xoá", 5, func(t *testing.T, inviter *User, invitee *User) {
			require.NoError(t, DB.Delete(&User{}, inviter.Id).Error)
		}},
		{"chưa xác nhận điều khoản thanh toán", 5, func(t *testing.T, inviter *User, invitee *User) {
			operation_setting.GetPaymentSetting().ComplianceConfirmed = false
		}},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enableReferralCommissionForTest(t, tc.percent)
			inviter := insertReferralUserForTest(t, 900+index*2, 0)
			invitee := insertReferralUserForTest(t, 901+index*2, inviter.Id)
			tc.prepare(t, inviter, invitee)
			order := insertReferralTopUpForTest(t, invitee.Id, fmt.Sprintf("REFSKIP%d", index), PaymentProviderPayOS, 10, 260000)

			_, err := RechargePayOS(order.TradeNo, 260000, "127.0.0.1")
			require.NoError(t, err)

			// Đơn nạp vẫn hoàn tất, chỉ là không có hoa hồng.
			assert.Equal(t, 10*500000, reloadReferralUserForTest(t, invitee.Id).Quota)
			assert.Equal(t, 0, reloadReferralUserForTest(t, inviter.Id).AffQuota)
			assert.Empty(t, referralCommissionsForTest(t))
		})
	}
}

func TestReferralCommissionRoundsDown(t *testing.T) {
	enableReferralCommissionForTest(t, 3.3)
	inviter := insertReferralUserForTest(t, 951, 0)
	invitee := insertReferralUserForTest(t, 952, inviter.Id)
	// Hạn mức thô 1001 (Creem): 3,3% = 33,033 -> 33; không bao giờ làm tròn lên.
	insertReferralTopUpForTest(t, invitee.Id, "REFROUND", PaymentProviderCreem, 1001, 1)

	require.NoError(t, RechargeCreem("REFROUND", "", "", "127.0.0.1"))

	assert.Equal(t, 33, reloadReferralUserForTest(t, inviter.Id).AffQuota)
}

func TestReferralCommissionSummaryListAndSeen(t *testing.T) {
	enableReferralCommissionForTest(t, 10)
	inviter := insertReferralUserForTest(t, 961, 0)
	otherInviter := insertReferralUserForTest(t, 962, 0)
	inviteeA := insertReferralUserForTest(t, 963, inviter.Id)
	inviteeB := insertReferralUserForTest(t, 964, inviter.Id)
	inviteeC := insertReferralUserForTest(t, 965, otherInviter.Id)
	for index, order := range []struct {
		userId int
		amount int64
	}{{inviteeA.Id, 2}, {inviteeA.Id, 4}, {inviteeB.Id, 6}, {inviteeC.Id, 8}} {
		tradeNo := fmt.Sprintf("REFSUM%d", index)
		insertReferralTopUpForTest(t, order.userId, tradeNo, PaymentProviderPayOS, order.amount, 1)
		_, err := RechargePayOS(tradeNo, 1, "127.0.0.1")
		require.NoError(t, err)
	}

	summary, err := GetReferralCommissionSummary(inviter.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(3), summary.TotalCount)
	assert.Equal(t, int64((2+4+6)*50000), summary.TotalQuota)
	assert.Equal(t, int64((2+4+6)*500000), summary.TotalTopUpQuota)
	assert.Equal(t, int64(2), summary.PayingInvitees)
	assert.Equal(t, int64(3), summary.UnseenCount)
	assert.Equal(t, int64((2+4+6)*50000), summary.UnseenQuota)

	all, err := GetReferralCommissionSummary(0)
	require.NoError(t, err)
	assert.Equal(t, int64(4), all.TotalCount)
	assert.Equal(t, int64(2), all.InviterCount)
	assert.Equal(t, int64(3), all.PayingInvitees)

	pageInfo := &common.PageInfo{Page: 1, PageSize: 2}
	items, total, err := GetReferralCommissions(inviter.Id, pageInfo)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	require.Len(t, items, 2)
	// Mới nhất trước.
	assert.Equal(t, inviteeB.Id, items[0].InviteeId)
	assert.Equal(t, 6*50000, items[0].Quota)

	points, err := GetRecentReferralCommissionPoints(inviter.Id, 0, 100)
	require.NoError(t, err)
	assert.Len(t, points, 3)

	top, err := GetTopReferrers(10)
	require.NoError(t, err)
	require.Len(t, top, 2)
	assert.Equal(t, inviter.Id, top[0].InviterId)
	assert.Equal(t, int64((2+4+6)*50000), top[0].TotalQuota)
	assert.Equal(t, int64(2), top[0].InviteeCount)
	assert.Equal(t, otherInviter.Id, top[1].InviterId)

	referred, err := CountReferredUsers()
	require.NoError(t, err)
	assert.Equal(t, int64(3), referred)

	// Đã xem: chỉ đánh dấu của đúng người mời đó.
	require.NoError(t, MarkReferralCommissionsSeen(inviter.Id))
	summary, err = GetReferralCommissionSummary(inviter.Id)
	require.NoError(t, err)
	assert.Zero(t, summary.UnseenCount)
	assert.Zero(t, summary.UnseenQuota)
	other, err := GetReferralCommissionSummary(otherInviter.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(1), other.UnseenCount)

	names, err := GetUsernamesByIds([]int{inviter.Id, inviteeC.Id, 99999})
	require.NoError(t, err)
	assert.Equal(t, map[int]string{inviter.Id: "referral_user_961", inviteeC.Id: "referral_user_965"}, names)
}
