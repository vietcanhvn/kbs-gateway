package controller

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Nhà cung cấp OAuth giả: lưu mã người dùng vào cột github_id.
type inviterTestOAuthProvider struct{}

func (inviterTestOAuthProvider) GetName() string { return "Test" }
func (inviterTestOAuthProvider) IsEnabled() bool { return true }
func (inviterTestOAuthProvider) ExchangeToken(context.Context, string, *gin.Context) (*oauth.OAuthToken, error) {
	return nil, nil
}
func (inviterTestOAuthProvider) GetUserInfo(context.Context, *oauth.OAuthToken) (*oauth.OAuthUser, error) {
	return nil, nil
}
func (inviterTestOAuthProvider) IsUserIDTaken(string) bool { return false }
func (inviterTestOAuthProvider) FillUserByProviderID(*model.User, string) error {
	return nil
}
func (inviterTestOAuthProvider) SetProviderUserID(user *model.User, providerUserID string) {
	user.GitHubId = providerUserID
}
func (inviterTestOAuthProvider) GetProviderPrefix() string    { return "test_" }
func (inviterTestOAuthProvider) ProviderUserIDColumn() string { return "github_id" }

func TestOAuthSignUpThroughReferralLinkRecordsTheInviter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedisEnabled, previousRegister := common.RedisEnabled, common.RegisterEnabled
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	common.RedisEnabled = false
	common.RegisterEnabled = true
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB, model.LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}))
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled, common.RegisterEnabled = previousRedisEnabled, previousRegister
		common.SetDatabaseTypes(previousMain, previousLog)
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	inviter := &model.User{Username: "inviter", Status: common.UserStatusEnabled, AffCode: "INV1"}
	require.NoError(t, db.Create(inviter).Error)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	invited, err := findOrCreateOAuthUser(c, inviterTestOAuthProvider{}, &oauth.OAuthUser{ProviderUserID: "p-1", Username: "invited"}, "INV1")
	require.NoError(t, err)
	organic, err := findOrCreateOAuthUser(c, inviterTestOAuthProvider{}, &oauth.OAuthUser{ProviderUserID: "p-2", Username: "organic"}, "")
	require.NoError(t, err)

	// Hoa hồng theo tiền nạp tra người mời từ cột inviter_id của tài khoản.
	var storedInvited, storedOrganic model.User
	require.NoError(t, db.First(&storedInvited, invited.Id).Error)
	assert.Equal(t, inviter.Id, storedInvited.InviterId)
	require.NoError(t, db.First(&storedOrganic, organic.Id).Error)
	assert.Zero(t, storedOrganic.InviterId)
}
