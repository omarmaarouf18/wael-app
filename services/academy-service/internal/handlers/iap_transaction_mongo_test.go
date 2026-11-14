package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/iap"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
)

func TestIAPTransaction_LiveMongo_EndToEndAndIdempotency(t *testing.T) {
	mongoURI := requireMongoURI(t)
	dbName := randomMongoDatabase("iap_live_test")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	st, err := store.NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("NewMongoStore failed: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cCancel()
		_ = st.Database().Drop(cleanupCtx)
		_ = st.Close(cleanupCtx)
	})

	if err := st.SeedLevels(ctx); err != nil {
		t.Fatalf("SeedLevels failed: %v", err)
	}

	ca := newIAPTestCA(t)
	s := New(st, "test", "test-gateway-secret", "test-internal-token", "http://auth-service:3002", false, "+201000000000")
	s.IAPVerifier = ca.verifier
	s.IAPBundleID = "com.wael.app"
	s.IAPSandboxAllowAll = true

	now := time.Now().UTC()
	futureExp := now.Add(30 * 24 * time.Hour)

	subj1 := &models.Subject{
		ID:              fmt.Sprintf("s-live-%d-1", time.Now().UnixNano()),
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "أحياء مباشر",
		TitleEn:         "Biology Live",
		Price:           150,
		Status:          models.StatusPublished,
		AccessExpiresAt: futureExp,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := st.CreateSubject(ctx, subj1); err != nil {
		t.Fatalf("CreateSubject 1: %v", err)
	}

	subj2 := &models.Subject{
		ID:              fmt.Sprintf("s-live-%d-2", time.Now().UnixNano()),
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "كيمياء مباشر",
		TitleEn:         "Chemistry Live",
		Price:           150,
		Status:          models.StatusPublished,
		AccessExpiresAt: futureExp,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := st.CreateSubject(ctx, subj2); err != nil {
		t.Fatalf("CreateSubject 2: %v", err)
	}

	pkg := &models.IAPPackage{
		ID:            "pkg_live_full",
		ProductID:     "com.wael.app.pkg.live",
		TitleAr:       "باقة البث المباشر",
		TitleEn:       "Live Package",
		DescriptionAr: "الوصف بالعربي",
		DescriptionEn: "English description",
		SubjectIDs:    []string{subj1.ID, subj2.ID},
		Position:      1,
		Published:     true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := st.CreateIAPPackage(ctx, pkg); err != nil {
		t.Fatalf("CreateIAPPackage: %v", err)
	}

	studentID := "usr_live_student_1"
	studentTok := makeStudentToken(t, studentID)
	accountToken := models.DeterministicAppAccountToken(studentID)

	nowMs := time.Now().UnixMilli()
	signedTx := ca.makeJWS(t, &iap.JWSTransactionPayload{
		TransactionID:         "tx_live_mongo_001",
		OriginalTransactionID: "tx_live_mongo_001",
		BundleID:              "com.wael.app",
		ProductID:             "com.wael.app.pkg.live",
		Type:                  "Non-Renewing Subscription",
		AppAccountToken:       accountToken,
		Environment:           "Sandbox",
		PurchaseDate:          nowMs,
		OriginalPurchaseDate:  nowMs,
		SignedDate:            nowMs,
	})

	// 1. First submission -> 200 granted
	rec1 := postTransaction(t, s, studentTok, "test-gateway-secret", map[string]string{
		"signed_transaction": signedTx,
	})
	if rec1.Code != http.StatusOK {
		t.Fatalf("first submit: expected 200, got %d (%s)", rec1.Code, rec1.Body.String())
	}
	var res1 IAPTransactionResponseDTO
	if err := json.Unmarshal(rec1.Body.Bytes(), &res1); err != nil {
		t.Fatalf("unmarshal res1: %v", err)
	}
	if res1.Status != "granted" {
		t.Fatalf("expected status 'granted', got %q", res1.Status)
	}
	if len(res1.GrantedSubjectIDs) != 2 {
		t.Fatalf("expected 2 granted subject IDs, got %v", res1.GrantedSubjectIDs)
	}

	// Verify entitlements in Mongo
	ents, err := st.ListEntitlementsByUser(ctx, studentID)
	if err != nil {
		t.Fatalf("ListEntitlementsByUser: %v", err)
	}
	if len(ents) != 2 {
		t.Fatalf("expected 2 entitlements in mongo, got %d", len(ents))
	}

	// Verify purchase in Mongo
	pur, err := st.GetIAPPurchaseByTransactionID(ctx, "tx_live_mongo_001")
	if err != nil || pur == nil {
		t.Fatalf("GetIAPPurchaseByTransactionID failed: err=%v, pur=%v", err, pur)
	}
	if pur.UserID != studentID || pur.ProductID != "com.wael.app.pkg.live" {
		t.Fatalf("stored purchase mismatch: %+v", pur)
	}

	// 2. Second submission by same student -> 200 already_processed with same payload
	rec2 := postTransaction(t, s, studentTok, "test-gateway-secret", map[string]string{
		"signed_transaction": signedTx,
	})
	if rec2.Code != http.StatusOK {
		t.Fatalf("second submit: expected 200, got %d (%s)", rec2.Code, rec2.Body.String())
	}
	var res2 IAPTransactionResponseDTO
	if err := json.Unmarshal(rec2.Body.Bytes(), &res2); err != nil {
		t.Fatalf("unmarshal res2: %v", err)
	}
	if res2.Status != "already_processed" {
		t.Fatalf("expected status 'already_processed', got %q", res2.Status)
	}
	if res2.TransactionID != res1.TransactionID || len(res2.GrantedSubjectIDs) != 2 {
		t.Fatalf("already_processed payload mismatch: %+v vs %+v", res2, res1)
	}

	// 3. Submission by different student with same transactionId -> 409 conflict
	otherStudentID := "usr_live_student_other"
	otherTok := makeStudentToken(t, otherStudentID)
	otherAccountToken := models.DeterministicAppAccountToken(otherStudentID)
	otherSignedTx := ca.makeJWS(t, &iap.JWSTransactionPayload{
		TransactionID:         "tx_live_mongo_001",
		OriginalTransactionID: "tx_live_mongo_001",
		BundleID:              "com.wael.app",
		ProductID:             "com.wael.app.pkg.live",
		Type:                  "Non-Renewing Subscription",
		AppAccountToken:       otherAccountToken,
		Environment:           "Sandbox",
		PurchaseDate:          nowMs,
		OriginalPurchaseDate:  nowMs,
		SignedDate:            nowMs,
	})
	rec3 := postTransaction(t, s, otherTok, "test-gateway-secret", map[string]string{
		"signed_transaction": otherSignedTx,
	})
	if rec3.Code != http.StatusConflict {
		t.Fatalf("expected 409 conflict, got %d (%s)", rec3.Code, rec3.Body.String())
	}
}

func TestIAPTransaction_LiveMongo_Concurrency_Race(t *testing.T) {
	mongoURI := requireMongoURI(t)
	dbName := randomMongoDatabase("iap_live_race")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	st, err := store.NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("NewMongoStore failed: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cCancel()
		_ = st.Database().Drop(cleanupCtx)
		_ = st.Close(cleanupCtx)
	})

	if err := st.SeedLevels(ctx); err != nil {
		t.Fatalf("SeedLevels failed: %v", err)
	}

	ca := newIAPTestCA(t)
	s := New(st, "test", "test-gateway-secret", "test-internal-token", "http://auth-service:3002", false, "+201000000000")
	s.IAPVerifier = ca.verifier
	s.IAPBundleID = "com.wael.app"
	s.IAPSandboxAllowAll = true

	now := time.Now().UTC()
	futureExp := now.Add(30 * 24 * time.Hour)

	subj := &models.Subject{
		ID:              fmt.Sprintf("s-live-race-%d", time.Now().UnixNano()),
		LevelKey:        "bachelor-y1",
		Term:            "first",
		TitleAr:         "مادة السباق",
		TitleEn:         "Race Subject",
		Price:           150,
		Status:          models.StatusPublished,
		AccessExpiresAt: futureExp,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := st.CreateSubject(ctx, subj); err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}

	pkg := &models.IAPPackage{
		ID:            "pkg_live_race",
		ProductID:     "com.wael.app.pkg.race",
		TitleAr:       "باقة السباق",
		TitleEn:       "Race Package",
		DescriptionAr: "الوصف",
		DescriptionEn: "Description",
		SubjectIDs:    []string{subj.ID},
		Position:      1,
		Published:     true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := st.CreateIAPPackage(ctx, pkg); err != nil {
		t.Fatalf("CreateIAPPackage: %v", err)
	}

	studentID := "usr_live_student_race"
	studentTok := makeStudentToken(t, studentID)
	accountToken := models.DeterministicAppAccountToken(studentID)

	nowMs := time.Now().UnixMilli()
	signedTx := ca.makeJWS(t, &iap.JWSTransactionPayload{
		TransactionID:         "tx_live_race_001",
		OriginalTransactionID: "tx_live_race_001",
		BundleID:              "com.wael.app",
		ProductID:             "com.wael.app.pkg.race",
		Type:                  "Non-Renewing Subscription",
		AppAccountToken:       accountToken,
		Environment:           "Sandbox",
		PurchaseDate:          nowMs,
		OriginalPurchaseDate:  nowMs,
		SignedDate:            nowMs,
	})

	const numGoroutines = 10
	var wg sync.WaitGroup
	startBarrier := make(chan struct{})

	var grantedCount int64
	var alreadyProcessedCount int64

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startBarrier

			rec := postTransaction(t, s, studentTok, "test-gateway-secret", map[string]string{
				"signed_transaction": signedTx,
			})
			if rec.Code != http.StatusOK {
				t.Errorf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
				return
			}
			var res IAPTransactionResponseDTO
			if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
				t.Errorf("unmarshal response: %v", err)
				return
			}
			if res.Status == "granted" {
				atomic.AddInt64(&grantedCount, 1)
			} else if res.Status == "already_processed" {
				atomic.AddInt64(&alreadyProcessedCount, 1)
			} else {
				t.Errorf("unexpected status: %s", res.Status)
			}
		}()
	}

	close(startBarrier)
	wg.Wait()

	if grantedCount != 1 {
		t.Fatalf("expected exactly 1 'granted', got %d (already_processed: %d)", grantedCount, alreadyProcessedCount)
	}
	if grantedCount+alreadyProcessedCount != int64(numGoroutines) {
		t.Fatalf("expected total %d successful answers, got %d", numGoroutines, grantedCount+alreadyProcessedCount)
	}

	// Verify only 1 entitlement in Mongo for this subject and user
	ents, err := st.ListEntitlementsByUser(ctx, studentID)
	if err != nil {
		t.Fatalf("ListEntitlementsByUser: %v", err)
	}
	if len(ents) != 1 {
		t.Fatalf("expected exactly 1 entitlement in mongo, got %d", len(ents))
	}

	// Owner 2026-11-14: only the winning grant writes an iap_purchase audit entry.
	if n := len(iapAuditEntries(t, s)); n != 1 {
		t.Fatalf("expected exactly 1 iap_purchase audit entry in mongo, got %d", n)
	}
}
