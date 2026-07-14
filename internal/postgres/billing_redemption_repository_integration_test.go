package postgres

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBillingRedemptionIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer pool.Close()

	adminID := insertIntegrationUser(t, pool, domain.UserRoleAdmin)
	userID := insertIntegrationUser(t, pool, domain.UserRoleUser)
	otherUserID := insertIntegrationUser(t, pool, domain.UserRoleUser)
	repository := NewBillingAccountRepository(pool)
	batch, err := repository.IssueRedemptionCodes(t.Context(), IssueRedemptionCodeParams{
		ActorUserID: adminID, ActorRole: domain.UserRoleAdmin, Currency: "USD",
		AmountNanos: 3_000_000_000, Quantity: 3, RequestID: "issue-redemption-batch",
	})
	if err != nil || len(batch) != 3 {
		t.Fatalf("issue redemption batch: count=%d err=%v", len(batch), err)
	}
	batchCodes := map[string]struct{}{}
	for _, item := range batch {
		batchCodes[item.Code] = struct{}{}
		if item.RedemptionCode.AmountNanos != 3_000_000_000 {
			t.Fatalf("unexpected batch item: %#v", item)
		}
	}
	if len(batchCodes) != 3 {
		t.Fatalf("batch contains duplicate plaintext codes: %#v", batch)
	}

	issued, err := repository.IssueRedemptionCode(t.Context(), IssueRedemptionCodeParams{
		ActorUserID: adminID,
		ActorRole:   domain.UserRoleAdmin,
		Currency:    "USD",
		AmountNanos: 5_000_000_000,
		RequestID:   "issue-redemption",
	})
	if err != nil {
		t.Fatalf("issue redemption code: %v", err)
	}
	if issued.Code == "" || issued.RedemptionCode.CodeHint == "" || strings.Contains(issued.RedemptionCode.CodeHint, issued.Code) {
		t.Fatalf("unexpected issued redemption code: %#v", issued)
	}
	var storedHash []byte
	var storedHint string
	if err := pool.QueryRow(t.Context(), `
		SELECT code_hash, code_hint FROM billing_redemption_codes WHERE id = $1::uuid
	`, issued.RedemptionCode.ID).Scan(&storedHash, &storedHint); err != nil {
		t.Fatalf("load stored redemption code: %v", err)
	}
	if len(storedHash) != 32 || strings.Contains(storedHint, issued.Code) {
		t.Fatalf("plaintext redemption code was stored: hint=%q hash=%x", storedHint, storedHash)
	}

	result, err := repository.RedeemCode(t.Context(), userID, domain.UserRoleUser, issued.Code, "redeem-request")
	if err != nil {
		t.Fatalf("redeem code: %v", err)
	}
	if result.Account.BalanceNanos != 5_000_000_000 || result.Transaction.Kind != domain.BillingTransactionRedemptionCredit {
		t.Fatalf("unexpected redemption result: %#v", result)
	}
	replayed, err := repository.RedeemCode(t.Context(), userID, domain.UserRoleUser, issued.Code, "redeem-retry")
	if err != nil || replayed.Transaction.ID != result.Transaction.ID {
		t.Fatalf("replay redemption: result=%#v err=%v", replayed, err)
	}
	if _, err := repository.RedeemCode(t.Context(), otherUserID, domain.UserRoleUser, issued.Code, "redeem-other"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("other user redemption error = %v", err)
	}

	items, _, err := repository.ListRedemptionCodes(t.Context(), RedemptionCodeListParams{Limit: 10})
	if err != nil {
		t.Fatalf("list redemption codes: %v", err)
	}
	found := false
	for _, item := range items {
		if item.ID == issued.RedemptionCode.ID {
			found = true
			if item.Status != domain.BillingRedemptionCodeRedeemed || item.RedeemedByUserID != userID || item.BillingTransactionID != result.Transaction.ID {
				t.Fatalf("unexpected listed redemption code: %#v", item)
			}
		}
	}
	if !found {
		t.Fatal("issued redemption code missing from admin list")
	}

	var redemptionTransactions int
	if err := pool.QueryRow(t.Context(), `
		SELECT count(*) FROM billing_transactions WHERE redemption_code_id = $1::uuid
	`, issued.RedemptionCode.ID).Scan(&redemptionTransactions); err != nil || redemptionTransactions != 1 {
		t.Fatalf("redemption transaction count=%d err=%v", redemptionTransactions, err)
	}

	concurrentCode, err := repository.IssueRedemptionCode(t.Context(), IssueRedemptionCodeParams{
		ActorUserID: adminID, ActorRole: domain.UserRoleAdmin, Currency: "USD",
		AmountNanos: 1_000_000_000, RequestID: "issue-concurrent-redemption",
	})
	if err != nil {
		t.Fatalf("issue concurrent redemption code: %v", err)
	}
	type redemptionResult struct {
		result *domain.BillingRedemptionResult
		err    error
	}
	results := make(chan redemptionResult, 2)
	for range 2 {
		go func() {
			result, err := repository.RedeemCode(t.Context(), userID, domain.UserRoleUser, concurrentCode.Code, "concurrent-redemption")
			results <- redemptionResult{result: result, err: err}
		}()
	}
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.result.Transaction.ID != second.result.Transaction.ID || first.result.Replayed == second.result.Replayed {
		t.Fatalf("concurrent redemption results: first=%#v second=%#v", first, second)
	}

	if _, err := repository.GetOrCreateAccount(t.Context(), otherUserID, "USD"); err != nil {
		t.Fatalf("create frozen redemption account: %v", err)
	}
	frozen := "frozen"
	if _, err := repository.UpdateAccount(t.Context(), otherUserID, "USD", &frozen); err != nil {
		t.Fatalf("freeze redemption account: %v", err)
	}
	frozenCode, err := repository.IssueRedemptionCode(t.Context(), IssueRedemptionCodeParams{
		ActorUserID: adminID, ActorRole: domain.UserRoleAdmin, Currency: "USD",
		AmountNanos: 1_000_000_000, RequestID: "issue-frozen-redemption",
	})
	if err != nil {
		t.Fatalf("issue frozen redemption code: %v", err)
	}
	if _, err := repository.RedeemCode(t.Context(), otherUserID, domain.UserRoleUser, frozenCode.Code, "frozen-redemption"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("frozen redemption error = %v", err)
	}
	active := "active"
	if _, err := repository.UpdateAccount(t.Context(), otherUserID, "USD", &active); err != nil {
		t.Fatalf("activate redemption account: %v", err)
	}
	if _, err := repository.RedeemCode(t.Context(), otherUserID, domain.UserRoleUser, frozenCode.Code, "active-redemption"); err != nil {
		t.Fatalf("redeem after account activation: %v", err)
	}

	expiresAt := time.Now().Add(100 * time.Millisecond)
	expiringCode, err := repository.IssueRedemptionCode(t.Context(), IssueRedemptionCodeParams{
		ActorUserID: adminID, ActorRole: domain.UserRoleAdmin, Currency: "USD",
		AmountNanos: 1_000_000_000, ExpiresAt: &expiresAt, RequestID: "issue-expiring-redemption",
	})
	if err != nil {
		t.Fatalf("issue expiring redemption code: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	if _, err := repository.RedeemCode(t.Context(), userID, domain.UserRoleUser, expiringCode.Code, "expired-redemption"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expired redemption error = %v", err)
	}

	disabledCode, err := repository.IssueRedemptionCode(t.Context(), IssueRedemptionCodeParams{
		ActorUserID: adminID, ActorRole: domain.UserRoleAdmin, Currency: "USD",
		AmountNanos: 2_000_000_000, RequestID: "issue-disabled-redemption",
	})
	if err != nil {
		t.Fatalf("issue code to disable: %v", err)
	}
	disabledItem, err := repository.DisableRedemptionCode(t.Context(), DisableRedemptionCodeParams{
		CodeID: disabledCode.RedemptionCode.ID, ActorUserID: adminID,
		ActorRole: domain.UserRoleAdmin, RequestID: "disable-redemption",
	})
	if err != nil || disabledItem.Status != domain.BillingRedemptionCodeDisabled {
		t.Fatalf("disable redemption code: item=%#v err=%v", disabledItem, err)
	}
	replayedDisable, err := repository.DisableRedemptionCode(t.Context(), DisableRedemptionCodeParams{
		CodeID: disabledCode.RedemptionCode.ID, ActorUserID: adminID,
		ActorRole: domain.UserRoleAdmin, RequestID: "disable-redemption-retry",
	})
	if err != nil || replayedDisable.Status != domain.BillingRedemptionCodeDisabled {
		t.Fatalf("replay redemption disable: item=%#v err=%v", replayedDisable, err)
	}
	if _, err := repository.RedeemCode(t.Context(), userID, domain.UserRoleUser, disabledCode.Code, "redeem-disabled"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("disabled redemption error = %v", err)
	}
	if _, err := repository.DisableRedemptionCode(t.Context(), DisableRedemptionCodeParams{
		CodeID: issued.RedemptionCode.ID, ActorUserID: adminID,
		ActorRole: domain.UserRoleAdmin, RequestID: "disable-redeemed",
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("disable redeemed code error = %v", err)
	}
}
