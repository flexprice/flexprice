package service

import (
	"context"
	"sort"
	"sync"

	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type lockStateKey struct{}

type heldLocks struct{ mus []*sync.Mutex }

// lockingDB fakes advisory locks.
type lockingDB struct {
	postgres.IClient
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (d *lockingDB) WithTx(ctx context.Context, fn func(context.Context) error) error {
	if ctx.Value(lockStateKey{}) != nil {
		return fn(ctx)
	}
	st := &heldLocks{}
	defer func() {
		for _, m := range st.mus {
			m.Unlock()
		}
	}()
	return d.IClient.WithTx(context.WithValue(ctx, lockStateKey{}, st), fn)
}

func (d *lockingDB) LockWithWait(ctx context.Context, req postgres.LockRequest) error {
	st, _ := ctx.Value(lockStateKey{}).(*heldLocks)
	d.mu.Lock()
	m, ok := d.locks[req.Key]
	if !ok {
		m = &sync.Mutex{}
		d.locks[req.Key] = m
	}
	d.mu.Unlock()
	m.Lock()
	if st != nil {
		st.mus = append(st.mus, m)
	}
	return nil
}

// cachedWalletRepo fakes the wallet cache.
type cachedWalletRepo struct {
	wallet.Repository
	mu     sync.Mutex
	cached *wallet.Wallet
	// recacheStale simulates a stale re-cache.
	recacheStale bool
}

func (r *cachedWalletRepo) GetWalletByID(ctx context.Context, id string) (*wallet.Wallet, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cached != nil {
		c := *r.cached
		return &c, nil
	}
	w, err := r.Repository.GetWalletByID(ctx, id)
	if err != nil {
		return nil, err
	}
	c := *w
	r.cached = &c
	return w, nil
}

func (r *cachedWalletRepo) UpdateWalletBalance(ctx context.Context, id string, bal, credit decimal.Decimal) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	before, err := r.Repository.GetWalletByID(ctx, id)
	if err != nil {
		return err
	}
	snap := *before
	if err := r.Repository.UpdateWalletBalance(ctx, id, bal, credit); err != nil {
		return err
	}
	r.cached = nil
	if r.recacheStale {
		r.cached = &snap
	}
	return nil
}

func (s *WalletServiceSuite) serviceWithCachedRepo(recacheStale bool) (WalletService, *cachedWalletRepo) {
	repo := &cachedWalletRepo{Repository: s.GetStores().WalletRepo, recacheStale: recacheStale}
	params := s.buildServiceParams()
	params.WalletRepo = repo
	params.DB = &lockingDB{IClient: s.GetDB(), locks: map[string]*sync.Mutex{}}
	return NewWalletService(params), repo
}

func (s *WalletServiceSuite) resetWalletAndSeed(svc WalletService, credits int64) {
	ctx := s.GetContext()
	id := s.testData.wallet.ID
	s.NoError(s.GetStores().WalletRepo.UpdateWalletBalance(ctx, id, decimal.Zero, decimal.Zero))
	s.NoError(svc.CreditWallet(ctx, &wallet.WalletOperation{
		WalletID:          id,
		Type:              types.TransactionTypeCredit,
		CreditAmount:      decimal.NewFromInt(credits),
		Description:       "seed",
		TransactionReason: types.TransactionReasonFreeCredit,
	}))
}

func (s *WalletServiceSuite) ledger() []*wallet.Transaction {
	f := types.NewWalletTransactionFilter()
	f.WalletID = &s.testData.wallet.ID
	txs, err := s.GetStores().WalletRepo.ListAllWalletTransactions(s.GetContext(), f)
	s.NoError(err)
	sort.SliceStable(txs, func(i, j int) bool { return txs[i].CreatedAt.Before(txs[j].CreatedAt) })
	return txs
}

func (s *WalletServiceSuite) TestOperationIgnoresStaleCachedWallet() {
	svc, repo := s.serviceWithCachedRepo(false)
	s.resetWalletAndSeed(svc, 1000)

	repo.mu.Lock()
	stale := *s.testData.wallet
	stale.CreditBalance = decimal.NewFromInt(5000)
	stale.Balance = decimal.NewFromInt(5000)
	repo.cached = &stale
	repo.mu.Unlock()

	s.NoError(svc.DebitWallet(s.GetContext(), &wallet.WalletOperation{
		WalletID:          s.testData.wallet.ID,
		Type:              types.TransactionTypeDebit,
		CreditAmount:      decimal.NewFromInt(10),
		Description:       "debit",
		TransactionReason: types.TransactionReasonInvoicePayment,
	}))

	txs := s.ledger()
	last := txs[len(txs)-1]
	s.True(decimal.NewFromInt(1000).Equal(last.CreditBalanceBefore), "before=%s", last.CreditBalanceBefore)
	s.True(decimal.NewFromInt(990).Equal(last.CreditBalanceAfter), "after=%s", last.CreditBalanceAfter)

	w, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallet.ID)
	s.NoError(err)
	s.True(decimal.NewFromInt(990).Equal(w.CreditBalance), "balance=%s", w.CreditBalance)
}

func (s *WalletServiceSuite) TestConcurrentOperationsKeepLedgerChain() {
	svc, _ := s.serviceWithCachedRepo(true)
	s.resetWalletAndSeed(svc, 1000)

	const n = 40
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx := s.GetContext()
			if i%2 == 0 {
				_ = svc.CreditWallet(ctx, &wallet.WalletOperation{
					WalletID:          s.testData.wallet.ID,
					Type:              types.TransactionTypeCredit,
					CreditAmount:      decimal.NewFromInt(7),
					Description:       "credit",
					TransactionReason: types.TransactionReasonFreeCredit,
				})
				return
			}
			_ = svc.DebitWallet(ctx, &wallet.WalletOperation{
				WalletID:          s.testData.wallet.ID,
				Type:              types.TransactionTypeDebit,
				CreditAmount:      decimal.NewFromInt(3),
				Description:       "debit",
				TransactionReason: types.TransactionReasonInvoicePayment,
			})
		}(i)
	}
	wg.Wait()

	sum := decimal.Zero
	prevAfter := decimal.Zero
	for i, tx := range s.ledger() {
		s.True(prevAfter.Equal(tx.CreditBalanceBefore), "tx %d before=%s prevAfter=%s", i, tx.CreditBalanceBefore, prevAfter)
		prevAfter = tx.CreditBalanceAfter
		if tx.Type == types.TransactionTypeCredit {
			sum = sum.Add(tx.CreditAmount)
		} else {
			sum = sum.Sub(tx.CreditAmount)
		}
	}

	w, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallet.ID)
	s.NoError(err)
	s.True(sum.Equal(w.CreditBalance), "ledger=%s wallet=%s", sum, w.CreditBalance)
}
