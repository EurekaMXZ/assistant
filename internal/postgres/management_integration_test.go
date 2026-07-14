package postgres

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	assistantauth "github.com/EurekaMXZ/assistant/internal/auth"
	assistantbilling "github.com/EurekaMXZ/assistant/internal/billing"
	"github.com/EurekaMXZ/assistant/internal/credential"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestManagementBillingAndAuditIntegration(t *testing.T) {
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
	users := NewUserRepository(pool)
	firstUsers, nextUsers, err := users.ListUsers(t.Context(), assistantauth.ListUsersParams{Limit: 2})
	if err != nil || len(firstUsers) != 2 || nextUsers == "" {
		t.Fatalf("first users page: users=%#v next=%q err=%v", firstUsers, nextUsers, err)
	}
	secondUsers, _, err := users.ListUsers(t.Context(), assistantauth.ListUsersParams{Limit: 2, Cursor: nextUsers})
	if err != nil || len(secondUsers) == 0 || secondUsers[0].ID == firstUsers[0].ID || secondUsers[0].ID == firstUsers[1].ID {
		t.Fatalf("second users page: users=%#v err=%v", secondUsers, err)
	}
	if _, _, err := users.ListUsers(t.Context(), assistantauth.ListUsersParams{Cursor: "invalid"}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("invalid users cursor error = %v", err)
	}

	cipher, err := credential.NewCipher(base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	if err != nil {
		t.Fatalf("new credential cipher: %v", err)
	}
	credentialID := uuid.NewString()
	encrypted, nonce, err := cipher.Encrypt(credentialID, domain.ProviderOpenAI, "sk-integration-secret")
	if err != nil {
		t.Fatalf("encrypt credential: %v", err)
	}
	credentials := NewProviderCredentialRepository(pool)
	createdCredential, err := credentials.Create(t.Context(), CreateProviderCredentialParams{
		ID: credentialID, Provider: domain.ProviderOpenAI, Name: "integration-" + credentialID,
		BaseURL: "https://api.example.com/v1", EncryptedAPIKey: encrypted, Nonce: nonce,
		KeyVersion: 1, KeyHint: credential.KeyHint("sk-integration-secret"), ActorUserID: adminID,
	})
	if err != nil {
		t.Fatalf("create credential: %v", err)
	}
	rawCredential, err := json.Marshal(createdCredential)
	if err != nil || string(rawCredential) == "" {
		t.Fatalf("marshal credential: %v", err)
	}
	if strings.Contains(string(rawCredential), "sk-integration-secret") {
		t.Fatalf("credential response contains plaintext key: %s", rawCredential)
	}
	stored, err := credentials.GetStored(t.Context(), credentialID)
	if err != nil {
		t.Fatalf("get stored credential: %v", err)
	}
	plaintext, err := cipher.Decrypt(stored.ID, stored.Provider, stored.EncryptedAPIKey, stored.Nonce)
	if err != nil || plaintext != "sk-integration-secret" {
		t.Fatalf("decrypt stored credential: plaintext=%q err=%v", plaintext, err)
	}

	models := NewModelRepository(pool)
	model, err := models.Create(t.Context(), CreateModelParams{
		Provider: domain.ProviderOpenAI, CredentialID: credentialID, Slug: "integration-" + credentialID,
		UpstreamModel: "gpt-integration-" + credentialID, DisplayName: "Integration Model",
		InputModalities: []string{"text"}, OutputModalities: []string{"text"}, SupportsTools: true,
		SupportedReasoningEfforts: []string{"low", "high"},
		ContextWindowTokens:       128000, MaxOutputTokens: 4096, DefaultParameters: json.RawMessage(`{"reasoning_effort":"low"}`),
		ActorUserID: adminID,
	})
	if err != nil {
		t.Fatalf("create model: %v", err)
	}
	price, err := models.CreatePrice(t.Context(), CreateModelPriceParams{
		ModelID: model.ID, Currency: "USD", InputPerMillionNanos: 1_000_000_000,
		CacheReadInputPerMillionNanos: 100_000_000, CacheCreationInputPerMillionNanos: 1_250_000_000,
		OutputPerMillionNanos: 2_000_000_000, ActorUserID: adminID,
	})
	if err != nil {
		t.Fatalf("create price: %v", err)
	}
	if price.CacheReadInputPerMillionNanos != 100_000_000 || price.CacheCreationInputPerMillionNanos != 1_250_000_000 {
		t.Fatalf("unexpected cache pricing: %#v", price)
	}
	var priceSnapshot map[string]any
	if err := json.Unmarshal(price.PricingSnapshot, &priceSnapshot); err != nil {
		t.Fatalf("decode pricing snapshot: %v", err)
	}
	if _, ok := priceSnapshot["cache_read_input_per_million_nanos"]; !ok {
		t.Fatalf("pricing snapshot missing cache read rate: %s", price.PricingSnapshot)
	}
	if _, ok := priceSnapshot["cache_creation_input_per_million_nanos"]; !ok {
		t.Fatalf("pricing snapshot missing cache creation rate: %s", price.PricingSnapshot)
	}
	price, err = models.SetPriceStatus(t.Context(), model.ID, price.ID, domain.ModelPriceStatusPublished, adminID, nil)
	if err != nil {
		t.Fatalf("publish price: %v", err)
	}
	for range 2 {
		if _, err := models.CreatePrice(t.Context(), CreateModelPriceParams{
			ModelID: model.ID, Currency: "USD", InputPerMillionNanos: 2_000_000_000,
			OutputPerMillionNanos: 3_000_000_000, ActorUserID: adminID,
		}); err != nil {
			t.Fatalf("create paginated price: %v", err)
		}
	}
	firstPrices, nextPrices, err := models.ListPrices(t.Context(), model.ID, 1, "")
	if err != nil || len(firstPrices) != 1 || nextPrices == "" {
		t.Fatalf("first prices page: prices=%#v next=%q err=%v", firstPrices, nextPrices, err)
	}
	secondPrices, _, err := models.ListPrices(t.Context(), model.ID, 1, nextPrices)
	if err != nil || len(secondPrices) != 1 || secondPrices[0].ID == firstPrices[0].ID {
		t.Fatalf("second prices page: prices=%#v err=%v", secondPrices, err)
	}
	if _, err := models.UpdateSettings(t.Context(), &model.ID, &model.ID, adminID); err != nil {
		t.Fatalf("update model settings: %v", err)
	}
	execution, err := models.ResolveExecution(t.Context(), "", false)
	if err != nil {
		t.Fatalf("resolve default model: %v", err)
	}
	if execution.ModelID != model.ID || execution.ModelPriceID != price.ID || execution.CredentialID != credentialID {
		t.Fatalf("unexpected execution snapshot: %#v", execution)
	}
	if len(execution.SupportedReasoningEfforts) != 2 || execution.SupportedReasoningEfforts[0] != "low" || execution.SupportedReasoningEfforts[1] != "high" {
		t.Fatalf("unexpected reasoning capabilities: %#v", execution.SupportedReasoningEfforts)
	}
	model, err = models.Update(t.Context(), UpdateModelParams{
		ID: model.ID, SupportedReasoningEfforts: []string{"xhigh"},
		DefaultParameters: json.RawMessage(`{"reasoning_effort":"xhigh"}`), ActorUserID: adminID,
	})
	if err != nil {
		t.Fatalf("update model reasoning capabilities: %v", err)
	}
	if len(model.SupportedReasoningEfforts) != 1 || model.SupportedReasoningEfforts[0] != "xhigh" {
		t.Fatalf("unexpected updated reasoning capabilities: %#v", model)
	}
	execution, err = models.ResolveExecution(t.Context(), model.ID, false)
	if err != nil || len(execution.SupportedReasoningEfforts) != 1 || execution.SupportedReasoningEfforts[0] != "xhigh" {
		t.Fatalf("resolve updated model: execution=%#v err=%v", execution, err)
	}

	billing := NewBillingAccountRepository(pool)
	toolPrices, err := billing.ListToolPrices(t.Context(), "USD")
	if err != nil || len(toolPrices) != len(domain.SupportedBillingToolKeys()) {
		t.Fatalf("list default tool prices: prices=%#v err=%v", toolPrices, err)
	}
	toolPriceVersions := make(map[string]int64, len(toolPrices))
	for _, price := range toolPrices {
		toolPriceVersions[price.ToolKey] = price.Version
	}
	toolPriceUpdate := UpdateBillingToolPricesParams{
		Currency: "USD", ActorUserID: adminID, ActorRole: domain.UserRoleAdmin, RequestID: "tool-price-update",
		Prices: []BillingToolPriceUpdate{
			{ToolKey: domain.BillingToolSandboxCreate, PricePerCallNanos: 250_000_000, Enabled: true, ExpectedVersion: toolPriceVersions[domain.BillingToolSandboxCreate]},
			{ToolKey: domain.BillingToolImageGeneration, PricePerCallNanos: 500_000_000, Enabled: true, ExpectedVersion: toolPriceVersions[domain.BillingToolImageGeneration]},
			{ToolKey: domain.BillingToolTavilySearch, PricePerCallNanos: 5_000_000, Enabled: true, ExpectedVersion: toolPriceVersions[domain.BillingToolTavilySearch]},
			{ToolKey: domain.BillingToolTavilyExtract, PricePerCallNanos: 10_000_000, Enabled: true, ExpectedVersion: toolPriceVersions[domain.BillingToolTavilyExtract]},
		},
	}
	toolPrices, err = billing.UpdateToolPrices(t.Context(), toolPriceUpdate)
	if err != nil || len(toolPrices) != 4 || toolPrices[0].PricePerCall != "0.25" || !toolPrices[0].Enabled {
		t.Fatalf("update tool prices: prices=%#v err=%v", toolPrices, err)
	}
	if _, err := billing.UpdateToolPrices(t.Context(), toolPriceUpdate); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale tool price update error = %v, want conflict", err)
	}
	account, err := billing.GetOrCreateAccount(t.Context(), userID, "USD")
	if err != nil {
		t.Fatalf("create billing account: %v", err)
	}
	unchanged, err := billing.GetOrCreateAccount(t.Context(), userID, "USD")
	if err != nil || unchanged.Version != account.Version {
		t.Fatalf("idempotent account read changed version: before=%d after=%d err=%v", account.Version, unchanged.Version, err)
	}
	topupParams := ManualBillingTransactionParams{
		UserID: userID, ActorUserID: adminID, ActorRole: domain.UserRoleAdmin, Currency: "USD",
		Kind: domain.BillingTransactionManualTopup, AmountNanos: 10_000_000_000,
		Reason: "integration topup", IdempotencyKey: "topup-" + userID, RequestID: "request-topup",
	}
	topup, err := billing.ApplyManualTransaction(t.Context(), topupParams)
	if err != nil {
		t.Fatalf("apply topup: %v", err)
	}
	replayed, err := billing.ApplyManualTransaction(t.Context(), topupParams)
	if err != nil || replayed.ID != topup.ID {
		t.Fatalf("replay topup: first=%q replay=%q err=%v", topup.ID, replayed.ID, err)
	}
	conflicting := topupParams
	conflicting.AmountNanos++
	if _, err := billing.ApplyManualTransaction(t.Context(), conflicting); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("conflicting idempotency error = %v", err)
	}
	concurrentParams := topupParams
	concurrentParams.AmountNanos = 1_000_000_000
	concurrentParams.IdempotencyKey = "concurrent-topup-" + userID
	type transactionResult struct {
		transaction *domain.BillingTransaction
		err         error
	}
	results := make(chan transactionResult, 2)
	for range 2 {
		go func() {
			transaction, err := billing.ApplyManualTransaction(t.Context(), concurrentParams)
			results <- transactionResult{transaction: transaction, err: err}
		}()
	}
	firstConcurrent, secondConcurrent := <-results, <-results
	if firstConcurrent.err != nil || secondConcurrent.err != nil || firstConcurrent.transaction.ID != secondConcurrent.transaction.ID {
		t.Fatalf("concurrent idempotency results: first=%#v second=%#v", firstConcurrent, secondConcurrent)
	}
	refundParams := ManualBillingTransactionParams{
		UserID: userID, ActorUserID: adminID, ActorRole: domain.UserRoleAdmin, Currency: "USD",
		Kind: domain.BillingTransactionManualRefund, AmountNanos: 3_000_000_000,
		Reason: "integration refund", IdempotencyKey: "refund-" + userID, RequestID: "request-refund",
	}
	refund, err := billing.ApplyManualTransaction(t.Context(), refundParams)
	if err != nil || refund.BalanceAfterNanos != 8_000_000_000 {
		t.Fatalf("apply refund: transaction=%#v err=%v", refund, err)
	}
	overRefund := refundParams
	overRefund.AmountNanos = 9_000_000_000
	overRefund.IdempotencyKey = "over-refund-" + userID
	if _, err := billing.ApplyManualTransaction(t.Context(), overRefund); !errors.Is(err, domain.ErrPaymentRequired) {
		t.Fatalf("over-refund error = %v", err)
	}
	status := "active"
	updatedAccount, err := billing.UpdateAccount(t.Context(), userID, "USD", &status)
	if err != nil {
		t.Fatalf("update billing account: account=%#v err=%v", updatedAccount, err)
	}
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin over-balance usage charge: %v", err)
	}
	if _, err := captureUsageCharge(t.Context(), tx, userID, &assistantbilling.Charge{Currency: "USD", AmountNanos: 9_000_000_000}); !errors.Is(err, domain.ErrPaymentRequired) {
		_ = tx.Rollback(t.Context())
		t.Fatalf("over-balance usage charge error = %v", err)
	}
	_ = tx.Rollback(t.Context())
	if _, err := pool.Exec(t.Context(), `UPDATE billing_transactions SET reason = 'mutated' WHERE id = $1::uuid`, topup.ID); err == nil {
		t.Fatal("append-only billing transaction allowed update")
	}

	conversationID := uuid.NewString()
	turnID := uuid.NewString()
	if _, err := pool.Exec(t.Context(), `INSERT INTO conversations (id, owner_user_id) VALUES ($1::uuid, $2::uuid)`, conversationID, userID); err != nil {
		t.Fatalf("insert compaction conversation: %v", err)
	}
	executionSnapshot, err := json.Marshal(execution)
	if err != nil {
		t.Fatalf("marshal compaction execution: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO turns (id, conversation_id, seq, status, model_id, model_revision, model_price_id, model_snapshot)
		VALUES ($1::uuid, $2::uuid, 1, $3, $4::uuid, $5, $6::uuid, $7::jsonb)
	`, turnID, conversationID, domain.TurnStatusContextReady, execution.ModelID, execution.ModelRevision, execution.ModelPriceID, executionSnapshot); err != nil {
		t.Fatalf("insert compaction turn: %v", err)
	}
	var balanceBefore int64
	if err := pool.QueryRow(t.Context(), `SELECT balance_nanos FROM billing_accounts WHERE user_id = $1::uuid AND currency = 'USD'`, userID).Scan(&balanceBefore); err != nil {
		t.Fatalf("load compaction balance: %v", err)
	}
	requestKey := "compaction-concurrent-" + turnID
	compactionResult := &llm.ModelResult{ResponseID: "compaction-response", Usage: llm.ModelUsage{InputTokens: 1000, OutputTokens: 1000, TotalTokens: 2000, Raw: json.RawMessage(`{"input_tokens":1000,"output_tokens":1000}`)}}
	compactionErrors := make(chan error, 2)
	for range 2 {
		go func() {
			compactionErrors <- billing.RecordCompactionUsage(t.Context(), conversationID, turnID, requestKey, *execution, compactionResult, "")
		}()
	}
	if firstErr, secondErr := <-compactionErrors, <-compactionErrors; firstErr != nil || secondErr != nil {
		t.Fatalf("concurrent compaction billing errors: first=%v second=%v", firstErr, secondErr)
	}
	var balanceAfter, usageAmount int64
	var usageCount, transactionCount int
	var usageTransactionID string
	if err := pool.QueryRow(t.Context(), `SELECT balance_nanos FROM billing_accounts WHERE user_id = $1::uuid AND currency = 'USD'`, userID).Scan(&balanceAfter); err != nil {
		t.Fatalf("load charged compaction balance: %v", err)
	}
	if err := pool.QueryRow(t.Context(), `
		SELECT count(*), max(amount_nanos), max(COALESCE(billing_transaction_id::text, ''))
		FROM billing_usage_events WHERE request_key = $1
	`, requestKey).Scan(&usageCount, &usageAmount, &usageTransactionID); err != nil {
		t.Fatalf("load compaction usage event: %v", err)
	}
	if usageTransactionID != "" {
		if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM billing_transactions WHERE id = $1::uuid`, usageTransactionID).Scan(&transactionCount); err != nil {
			t.Fatalf("load compaction transaction: %v", err)
		}
	}
	if usageCount != 1 || transactionCount != 1 || balanceBefore-balanceAfter != usageAmount {
		t.Fatalf("compaction charge count=%d transactions=%d before=%d after=%d amount=%d", usageCount, transactionCount, balanceBefore, balanceAfter, usageAmount)
	}
	usageEvents, _, err := billing.ListUsageEvents(t.Context(), BillingListParams{UserID: userID, Limit: 50})
	if err != nil {
		t.Fatalf("list usage events after tool billing migration: %v", err)
	}
	var compactionUsage *domain.BillingUsageEvent
	for index := range usageEvents {
		if usageEvents[index].RequestKey == requestKey {
			compactionUsage = &usageEvents[index]
			break
		}
	}
	if compactionUsage == nil || compactionUsage.ToolAmountNanos != 0 || compactionUsage.ToolAmount != "0.00" || string(compactionUsage.ToolUsage) != "{}" {
		t.Fatalf("compaction tool defaults = %#v", compactionUsage)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE billing_accounts SET status = 'frozen' WHERE user_id = $1::uuid AND currency = 'USD'`, userID); err != nil {
		t.Fatalf("freeze billing account: %v", err)
	}
	frozenTx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin frozen usage charge: %v", err)
	}
	if _, err := captureUsageCharge(t.Context(), frozenTx, userID, &assistantbilling.Charge{Currency: "USD", AmountNanos: 1}); !errors.Is(err, domain.ErrPaymentRequired) {
		_ = frozenTx.Rollback(t.Context())
		t.Fatalf("frozen usage account error = %v, want payment required", err)
	}
	_ = frozenTx.Rollback(t.Context())
	if _, err := pool.Exec(t.Context(), `UPDATE billing_accounts SET status = 'active' WHERE user_id = $1::uuid AND currency = 'USD'`, userID); err != nil {
		t.Fatalf("unfreeze billing account: %v", err)
	}

	audits := NewAuditRepository(pool)
	visible, _, err := audits.List(t.Context(), AuditListParams{ViewerUserID: userID, ViewerRole: domain.UserRoleUser, Limit: 50})
	if err != nil {
		t.Fatalf("list user audit events: %v", err)
	}
	if len(visible) < 2 {
		t.Fatalf("user audit events = %d, want topup and refund", len(visible))
	}
	unrelated, err := audits.Record(t.Context(), RecordAuditParams{
		ActorUserID: adminID, ActorRole: domain.UserRoleAdmin, SubjectUserID: otherUserID,
		Action: "integration.unrelated", Outcome: "succeeded", VisibleToSubject: true,
	})
	if err != nil {
		t.Fatalf("record unrelated audit: %v", err)
	}
	if _, err := audits.Get(t.Context(), unrelated.ID, userID, domain.UserRoleUser); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unrelated user audit lookup error = %v", err)
	}
	systemOnly, err := audits.Record(t.Context(), RecordAuditParams{
		ActorUserID: adminID, ActorRole: domain.UserRoleSystem,
		Action: "integration.system", ResourceType: "smtp_settings", Outcome: "succeeded",
		RequiredRole: domain.UserRoleSystem,
	})
	if err != nil {
		t.Fatalf("record system audit: %v", err)
	}
	adminEvents, _, err := audits.List(t.Context(), AuditListParams{ViewerUserID: adminID, ViewerRole: domain.UserRoleAdmin, Limit: 50})
	if err != nil {
		t.Fatalf("list admin audit events: %v", err)
	}
	for _, event := range adminEvents {
		if event.ID == systemOnly.ID {
			t.Fatal("admin audit list exposed system event")
		}
	}
	if _, err := audits.Get(t.Context(), systemOnly.ID, adminID, domain.UserRoleAdmin); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("admin system audit lookup error = %v", err)
	}
	if event, err := audits.Get(t.Context(), systemOnly.ID, adminID, domain.UserRoleSystem); err != nil || event.RequiredRole != domain.UserRoleSystem {
		t.Fatalf("system audit lookup event=%#v err=%v", event, err)
	}
	if _, err := pool.Exec(t.Context(), `DELETE FROM audit_events WHERE id = $1::uuid`, unrelated.ID); err == nil {
		t.Fatal("append-only audit event allowed delete")
	}
	overview := NewAdminOverviewRepository(pool)
	counts, err := overview.GetCounts(t.Context(), false)
	if err != nil || counts.Users < 3 || counts.EnabledModels < 1 || counts.ActiveAccounts < 1 || counts.AuditEvents < 1 {
		t.Fatalf("admin overview counts=%#v err=%v", counts, err)
	}
}

func insertIntegrationUser(t *testing.T, pool *pgxpool.Pool, role string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := pool.Exec(t.Context(), `
		INSERT INTO users (id, email, username, password_hash, role)
		VALUES ($1::uuid, $2, $3, 'integration-test', $4)
	`, id, id+"@example.com", "integration-"+id, role)
	if err != nil {
		t.Fatalf("insert integration user: %v", err)
	}
	return id
}
