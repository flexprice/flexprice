// Paddle-only sandbox: two catalog products (Flexprice Fixed Fee, Flexprice Usage Fee) plus a
// recurring $0 card-capture price. Exactly one Paddle customer per run unless you reuse one via env.
//
// Environment:
//   - SANDBOX_SUBSCRIPTION_ID — if set, only runs CreateSubscriptionCharge ($15+$60); skips $0 checkout.
//   - SANDBOX_CUSTOMER_ID — if set, reuses this customer instead of creating a new one (validates via GetCustomer).
//   - SANDBOX_ADDRESS_ID — optional with SANDBOX_CUSTOMER_ID; validates via GetAddress. If omitted,
//     uses the first listed address or creates a sandbox US address on that customer (needed for $0 txn path).
//
// The tool prints shell export snippets (SANDBOX_CUSTOMER_ID, SANDBOX_ADDRESS_ID, SANDBOX_SUBSCRIPTION_ID)
// twice on a full bootstrap run: immediately after resolving/creating customer+address, and again once
// subscription_id is known and the $15+$60 charge has succeeded (copy those for NEXT RUN).
//
// Default flow when SANDBOX_SUBSCRIPTION_ID is unset:
//   $0 subscription checkout → poll GetTransaction for subscription_id → CreateSubscriptionCharge on that sub.
//
// Run:
//
//	export PADDLE_API_KEY=pdl_sdbx_...
//	go run ./cmd/paddle-sandbox
//
// Reuse checkout + subscription (charge-only — skips catalog bootstrap):
//
//	export SANDBOX_CUSTOMER_ID=ctm_...
//	export SANDBOX_SUBSCRIPTION_ID=sub_...
//	go run ./cmd/paddle-sandbox
//
// After first bootstrap:
//
//	export SANDBOX_PRODUCT_ID_FIXED_FEE=pro_...
//	export SANDBOX_PRODUCT_ID_USAGE_FEE=pro_...
//
// Optional:
//
//	export SANDBOX_PRICE_ID_CARD_CAPTURE_ZERO=pri_...
//	export SANDBOX_MAX_WAIT_SUBSCRIPTION_SEC=300   # poll GetTransaction (default 300, 0 = no wait)
//
// Without card-capture price env: first recurring price on fixed-fee product is reused, or one is created.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/PaddleHQ/paddle-go-sdk/v5"
)

const (
	envProductFixedFee               = "SANDBOX_PRODUCT_ID_FIXED_FEE"
	envProductUsageFee               = "SANDBOX_PRODUCT_ID_USAGE_FEE"
	envPriceCardCaptureZero          = "SANDBOX_PRICE_ID_CARD_CAPTURE_ZERO"
	envCustomerID                    = "SANDBOX_CUSTOMER_ID"
	envAddressID                     = "SANDBOX_ADDRESS_ID"
	envSubscriptionID                = "SANDBOX_SUBSCRIPTION_ID"
	envMaxWaitSubscriptionSec        = "SANDBOX_MAX_WAIT_SUBSCRIPTION_SEC"
	defaultMaxWaitSubscriptionSec    = 300
	pollTransactionSubscriptionSleep = 2 * time.Second

	maxCatalogProducts = 2

	// Scenario amounts (USD, minor units / cents).
	centsUSD15 = int64(1500)
	centsUSD60 = int64(6000)

	productDisplayFixedFee = "Flexprice Fixed Fee"
	productDisplayUsageFee = "Flexprice Usage Fee"
)

func main() {
	log.SetFlags(0)
	apiKey := strings.TrimSpace(os.Getenv("PADDLE_API_KEY"))
	if apiKey == "" {
		log.Fatal("set PADDLE_API_KEY (sandbox: pdl_sdbx_…)")
	}

	baseURL := paddle.ProductionBaseURL
	if strings.HasPrefix(apiKey, "pdl_sdbx_") {
		baseURL = "https://sandbox-api.paddle.com"
	}

	client, err := paddle.New(apiKey, paddle.WithBaseURL(baseURL))
	if err != nil {
		log.Fatalf("paddle.New: %v", err)
	}

	ctx := context.Background()

	log.Printf("Paddle API base: %s", baseURL)

	maxWait := maxWaitSubscriptionFromEnv()
	collection := paddle.CollectionModeAutomatic
	curr := paddle.CurrencyCodeUSD

	subReuse := strings.TrimSpace(os.Getenv(envSubscriptionID))

	if subReuse != "" {
		runSubscriptionChargeOnly(ctx, client, subReuse)
		fmt.Printf("\nDone (SANDBOX_SUBSCRIPTION_ID — subscription charge only).\n")
		return
	}

	fixedFeeProd, usageFeeProd, zeroPriceID, err := ensureCatalog(ctx, client)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("catalog: products fixed=%s usage=%s | card_capture_zero pri=%s", fixedFeeProd, usageFeeProd, zeroPriceID)

	runTag := strconv.FormatInt(time.Now().UnixNano(), 36)
	customerID, addressID, err := resolveCustomerAndAddress(ctx, client, runTag)
	if err != nil {
		log.Fatalf("customer: %v", err)
	}

	printSandboxExportsBanner(
		"Export now — customer + address (subscription_id shown after checkout + charge)",
		customerID, addressID, "",
	)

	runCardCaptureThenSubscriptionCharge(ctx, client, maxWait, customerID, addressID, zeroPriceID, curr, collection)

	if maxWait > 0 {
		fmt.Printf("\nDone. Polled GetTransaction up to %s for subscription_id (SANDBOX_MAX_WAIT_SUBSCRIPTION_SEC).\n",
			maxWait.Round(time.Millisecond))
	} else {
		fmt.Printf("\nDone. SANDBOX_MAX_WAIT_SUBSCRIPTION_SEC=0 — only checked CreateTransaction response for subscription_id (no polling).\n")
	}
}

func maxWaitSubscriptionFromEnv() time.Duration {
	v := strings.TrimSpace(os.Getenv(envMaxWaitSubscriptionSec))
	if v == "" {
		return time.Duration(defaultMaxWaitSubscriptionSec) * time.Second
	}
	sec, err := strconv.Atoi(v)
	if err != nil || sec <= 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}

// printSandboxExportsBanner prints shell exports for SANDBOX_CUSTOMER_ID, SANDBOX_ADDRESS_ID, SANDBOX_SUBSCRIPTION_ID.
// Pass subscriptionID="" until checkout has created the Paddle subscription on the txn.
func printSandboxExportsBanner(title, customerID, addressID, subscriptionID string) {
	log.Println(separatorExport())
	log.Printf("%s\n", title)
	log.Println(separatorExport())
	if customerID != "" {
		log.Printf("export %s=%s\n", envCustomerID, customerID)
	}
	if addressID != "" {
		log.Printf("export %s=%s\n", envAddressID, addressID)
	}
	if subscriptionID != "" {
		log.Printf("export %s=%s\n", envSubscriptionID, subscriptionID)
	} else {
		log.Printf("# export %s=sub_...   # filled in the second export block after checkout completes this run\n", envSubscriptionID)
	}
	log.Println()
	log.Printf("go run ./cmd/paddle-sandbox   # charge-only: export %s only (skips catalog bootstrap)\n", envSubscriptionID)
	log.Println(separatorExport())
}

func separatorExport() string {
	return "======================================================================"
}

func resolveCustomerAndAddress(ctx context.Context, client *paddle.SDK, runTag string) (customerID, addressID string, err error) {
	if id := strings.TrimSpace(os.Getenv(envCustomerID)); id != "" {
		if _, err := client.GetCustomer(ctx, &paddle.GetCustomerRequest{CustomerID: id}); err != nil {
			return "", "", fmt.Errorf("%s=%s GetCustomer: %w", envCustomerID, id, err)
		}
		addrID, err := ensureAddressForCustomer(ctx, client, id)
		if err != nil {
			return "", "", err
		}
		log.Printf("reuse customer_id=%s address_id=%s", id, addrID)
		return id, addrID, nil
	}

	email := fmt.Sprintf("paddle_sandbox_%s@example.com", runTag)
	return createNewSandboxCustomer(ctx, client, email, "Paddle Sandbox")
}

func ensureAddressForCustomer(ctx context.Context, client *paddle.SDK, customerID string) (string, error) {
	if aid := strings.TrimSpace(os.Getenv(envAddressID)); aid != "" {
		if _, err := client.GetAddress(ctx, &paddle.GetAddressRequest{
			CustomerID: customerID,
			AddressID:  aid,
		}); err != nil {
			return "", fmt.Errorf("%s=%s GetAddress: %w", envAddressID, aid, err)
		}
		return aid, nil
	}

	pp := 50
	col, err := client.ListAddresses(ctx, &paddle.ListAddressesRequest{
		CustomerID: customerID,
		PerPage:    &pp,
	})
	if err != nil {
		return "", fmt.Errorf("ListAddresses: %w", err)
	}
	if col == nil {
		return "", errors.New("unexpected nil address collection")
	}
	var pick string
	if iterErr := col.Iter(ctx, func(a *paddle.Address) (bool, error) {
		if a == nil || a.ID == "" {
			return true, nil
		}
		if pick == "" {
			pick = a.ID
		}
		return true, nil
	}); iterErr != nil {
		return "", fmt.Errorf("ListAddresses iterate: %w", iterErr)
	}
	if pick != "" {
		log.Printf("using first listed address id=%s (set %s to override)", pick, envAddressID)
		return pick, nil
	}

	addr, err := createSandboxAddress(ctx, client, customerID)
	if err != nil {
		return "", err
	}
	log.Printf("created sandbox address id=%s for customer", addr.ID)
	return addr.ID, nil
}

func createSandboxAddress(ctx context.Context, client *paddle.SDK, customerID string) (*paddle.Address, error) {
	country := strings.TrimSpace(os.Getenv("SANDBOX_ADDRESS_COUNTRY"))
	if country == "" {
		country = "US"
	}
	return client.CreateAddress(ctx, &paddle.CreateAddressRequest{
		CustomerID:  customerID,
		CountryCode: paddle.CountryCode(strings.ToUpper(country)),
		Description: paddle.PtrTo("sandbox"),
		FirstLine:   paddle.PtrTo("1 Test Street"),
		City:        paddle.PtrTo("San Francisco"),
		PostalCode:  paddle.PtrTo("94102"),
		Region:      paddle.PtrTo("CA"),
	})
}

func createNewSandboxCustomer(ctx context.Context, client *paddle.SDK, email, name string) (customerID, addressID string, err error) {
	cust, err := client.CreateCustomer(ctx, &paddle.CreateCustomerRequest{
		Email:  email,
		Name:   paddle.PtrTo(name),
		Locale: paddle.PtrTo("en"),
	})
	if err != nil {
		return "", "", fmt.Errorf("CreateCustomer: %w", err)
	}

	addr, err := createSandboxAddress(ctx, client, cust.ID)
	if err != nil {
		return "", "", fmt.Errorf("CreateAddress: %w", err)
	}

	log.Printf("new customer=%s address=%s email=%s", cust.ID, addr.ID, email)
	return cust.ID, addr.ID, nil
}

func runSubscriptionChargeOnly(ctx context.Context, client *paddle.SDK, subID string) {
	sub, err := client.GetSubscription(ctx, &paddle.GetSubscriptionRequest{SubscriptionID: subID})
	if err != nil {
		log.Fatalf("%s=%s GetSubscription: %v", envSubscriptionID, subID, err)
	}

	if want := strings.TrimSpace(os.Getenv(envCustomerID)); want != "" && sub.CustomerID != want {
		log.Fatalf("%s belongs to customer %s, but %s=%s — mismatch",
			subID, sub.CustomerID, envCustomerID, want)
	}

	log.Printf("%s validated — customer=%s status=%s", subID, sub.CustomerID, sub.Status)

	items := []paddle.CreateSubscriptionChargeItems{
		*subscriptionChargeUSDLine(productDisplayFixedFee, "Fixed fee $15", centsUSD15),
		*subscriptionChargeUSDLine(productDisplayUsageFee, "Usage fee $60", centsUSD60),
	}
	if err := postSubscriptionCharge(ctx, client, subID, "sandbox_inv2", "fixed_15_usage_60_sub_charge", items); err != nil {
		log.Fatalf("CreateSubscriptionCharge: %v", err)
	}

	addrID := strings.TrimSpace(sub.AddressID)
	printSandboxExportsBanner(
		"NEXT RUN — subscription charge path (from this run)",
		strings.TrimSpace(sub.CustomerID), addrID, subID,
	)
}

func runCardCaptureThenSubscriptionCharge(
	ctx context.Context,
	client *paddle.SDK,
	maxPoll time.Duration,
	customerID, addressID, zeroPriceID string,
	curr paddle.CurrencyCode,
	collection paddle.CollectionMode,
) {
	txn, err := createCheckoutTransaction(ctx, client, customerID, addressID, curr, collection,
		[]paddle.CreateTransactionItems{
			*paddle.NewCreateTransactionItemsTransactionItemFromCatalog(&paddle.TransactionItemFromCatalog{
				PriceID:  zeroPriceID,
				Quantity: 1,
			}),
		},
		"sandbox_inv1", "$0_card_capture_only")
	if err != nil {
		log.Fatalf("CreateTransaction inv1: %v", err)
	}
	url := checkoutURL(txn)
	log.Printf("inv1 txn id=%s checkout=%s", txn.ID, url)

	subID := deref(txn.SubscriptionID)
	if subID == "" && maxPoll > 0 {
		log.Printf("polling GetTransaction(up to %s) for subscription_id on %s …", maxPoll.Round(time.Millisecond), txn.ID)
		subID = waitForTransactionSubscriptionID(ctx, client, txn.ID, maxPoll)
	}

	if subID == "" {
		log.Fatalf("transaction %s has no subscription_id yet — complete checkout in browser, "+
			"or increase %s", txn.ID, envMaxWaitSubscriptionSec)
	}

	log.Printf("reused subscription_id from txn %s → %s", txn.ID, subID)

	items := []paddle.CreateSubscriptionChargeItems{
		*subscriptionChargeUSDLine(productDisplayFixedFee, "Fixed fee $15", centsUSD15),
		*subscriptionChargeUSDLine(productDisplayUsageFee, "Usage fee $60", centsUSD60),
	}
	if err := postSubscriptionCharge(ctx, client, subID, "sandbox_inv2", "fixed_15_usage_60_sub_charge", items); err != nil {
		log.Fatalf("CreateSubscriptionCharge: %v", err)
	}

	printSandboxExportsBanner(
		"NEXT RUN — full reuse (recommended exports after successful charge)",
		customerID, addressID, subID,
	)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	x := strings.TrimSpace(*s)
	return x
}

// waitForTransactionSubscriptionID polls GetTransaction until SubscriptionID is set or deadline.
func waitForTransactionSubscriptionID(ctx context.Context, client *paddle.SDK, txnID string, maxWait time.Duration) string {
	if maxWait <= 0 {
		return ""
	}
	deadline := time.Now().Add(maxWait)
	for {
		txn, err := client.GetTransaction(ctx, &paddle.GetTransactionRequest{TransactionID: txnID})
		if err != nil {
			log.Printf("GetTransaction(%s): %v", txnID, err)
			if time.Now().After(deadline) {
				return ""
			}
			if !sleepPolling(ctx, deadline) {
				return ""
			}
			continue
		}
		if id := deref(txn.SubscriptionID); id != "" {
			return id
		}
		if time.Now().After(deadline) {
			return ""
		}
		if !sleepPolling(ctx, deadline) {
			return ""
		}
	}
}

func sleepPolling(ctx context.Context, deadline time.Time) bool {
	remaining := time.Until(deadline)
	step := pollTransactionSubscriptionSleep
	if step > remaining {
		step = remaining
		if step <= 0 {
			return false
		}
	}
	t := time.NewTimer(step)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func postSubscriptionCharge(
	ctx context.Context,
	client *paddle.SDK,
	subID, scenarioLabel, sandboxKind string,
	items []paddle.CreateSubscriptionChargeItems,
) error {
	onFailure := paddle.SubscriptionOnPaymentFailurePreventChange
	sub, err := client.CreateSubscriptionCharge(ctx, &paddle.CreateSubscriptionChargeRequest{
		SubscriptionID:   subID,
		EffectiveFrom:    paddle.EffectiveFromImmediately,
		Items:            items,
		OnPaymentFailure: &onFailure,
	})
	if err != nil {
		return err
	}
	log.Printf("subscription charge %s (%s): subscription_id=%s status=%s — billed on existing sub (saved payment method)", scenarioLabel, sandboxKind, subID, sub.Status)
	return nil
}

func subscriptionChargeUSDLine(productName, description string, cents int64) *paddle.CreateSubscriptionChargeItems {
	qty := paddle.PriceQuantity{Minimum: 1, Maximum: 100}
	return paddle.NewCreateSubscriptionChargeItemsSubscriptionChargeItemCreateWithProduct(&paddle.SubscriptionChargeItemCreateWithProduct{
		Quantity: 1,
		Price: paddle.SubscriptionChargeCreateWithProduct{
			Description: description,
			Name:        paddle.PtrTo(productName),
			UnitPrice: paddle.Money{
				Amount:       fmt.Sprintf("%d", cents),
				CurrencyCode: paddle.CurrencyCodeUSD,
			},
			Quantity: qty,
			Product: paddle.TransactionSubscriptionProductCreate{
				Name:        productName,
				TaxCategory: paddle.TaxCategoryStandard,
			},
		},
	})
}

func createCheckoutTransaction(
	ctx context.Context,
	client *paddle.SDK,
	customerID, addressID string,
	curr paddle.CurrencyCode,
	collection paddle.CollectionMode,
	items []paddle.CreateTransactionItems,
	scenarioLabel, sandboxKind string,
) (*paddle.Transaction, error) {
	return client.CreateTransaction(ctx, &paddle.CreateTransactionRequest{
		CustomerID:     paddle.PtrTo(customerID),
		AddressID:      paddle.PtrTo(addressID),
		CurrencyCode:   paddle.PtrTo(curr),
		CollectionMode: paddle.PtrTo(collection),
		Items:          items,
		CustomData: paddle.CustomData{
			"sandbox_scenario": scenarioLabel,
			"sandbox_kind":     sandboxKind,
		},
	})
}

func checkoutURL(txn *paddle.Transaction) string {
	if txn == nil || txn.Checkout == nil || txn.Checkout.URL == nil {
		return ""
	}
	return *txn.Checkout.URL
}

func boolPtr(b bool) *bool {
	return &b
}

// ensureCatalog creates or resolves Flexprice Fixed Fee + Usage Fee products and the recurring $0 card-capture price.
func ensureCatalog(ctx context.Context, client *paddle.SDK) (fixedFeeProdID, usageFeeProdID, zeroPriceID string, err error) {
	cat := &twoProductCatalog{client: client}
	fixedFeeProdID, err = cat.ensureProductID(ctx, envProductFixedFee, productDisplayFixedFee,
		"$0 recurring (card capture) and one-time fixed charges; attach more pri_ as needed.")
	if err != nil {
		return "", "", "", err
	}
	usageFeeProdID, err = cat.ensureProductID(ctx, envProductUsageFee, productDisplayUsageFee,
		"One-time usage charges; attach more pri_ per invoice.")
	if err != nil {
		return "", "", "", err
	}
	if fixedFeeProdID == usageFeeProdID {
		return "", "", "", fmt.Errorf(
			"fixed-fee and usage-fee product IDs must differ (%s vs %s must not be equal)",
			envProductFixedFee, envProductUsageFee,
		)
	}

	log.Printf("products: flexprice_fixed_fee=%s flexprice_usage_fee=%s (max %d)", fixedFeeProdID, usageFeeProdID, maxCatalogProducts)

	zeroPriceID, err = resolveZeroPrice(ctx, client, fixedFeeProdID)
	if err != nil {
		return "", "", "", err
	}

	logPrintedBootstrapFooter(cat.newProductsLog, fixedFeeProdID, usageFeeProdID, zeroPriceID)

	return fixedFeeProdID, usageFeeProdID, zeroPriceID, nil
}

type createdProduct struct {
	DisplayName string
	EnvKey      string
	ID          string
}

type twoProductCatalog struct {
	client         *paddle.SDK
	productsCreate int
	newProductsLog []createdProduct
}

func (c *twoProductCatalog) ensureProductID(ctx context.Context, envKey, defaultName, description string) (string, error) {
	id := strings.TrimSpace(os.Getenv(envKey))
	if id != "" {
		if _, err := c.client.GetProduct(ctx, &paddle.GetProductRequest{ProductID: id}); err != nil {
			return "", fmt.Errorf("%s=%s: %w", envKey, id, err)
		}
		log.Printf("reuse product_id=%s (from %s)", id, envKey)
		return id, nil
	}
	if c.productsCreate >= maxCatalogProducts {
		return "", fmt.Errorf("refusing to create product %q: already created %d (max %d). Set %s and %s",
			defaultName, c.productsCreate, maxCatalogProducts, envProductFixedFee, envProductUsageFee)
	}
	p, err := c.client.CreateProduct(ctx, &paddle.CreateProductRequest{
		Name:        defaultName,
		TaxCategory: paddle.TaxCategoryStandard,
		Description: paddle.PtrTo(description),
	})
	if err != nil {
		return "", fmt.Errorf("CreateProduct %s: %w", defaultName, err)
	}
	c.productsCreate++
	c.newProductsLog = append(c.newProductsLog, createdProduct{
		DisplayName: defaultName,
		EnvKey:      envKey,
		ID:          p.ID,
	})
	logBootstrapAfterProductCreated(defaultName, envKey, p.ID)
	return p.ID, nil
}

const bannerLine = "======================================================================"

func logBootstrapAfterProductCreated(displayName, envKey, productID string) {
	log.Print(bannerLine)
	log.Printf("NEW PADDLE PRODUCT — \"%s\"", displayName)
	log.Printf("  product_id : %s", productID)
	log.Printf("  set env key: %s", envKey)
	log.Printf("  In Terminal, run:")
	log.Printf("    export %s=%s", envKey, productID)
	log.Print(bannerLine + "\n")
}

func logPrintedBootstrapFooter(created []createdProduct, fixedFeeProdID, usageFeeProdID, zeroPri string) {
	if len(created) == 0 {
		return
	}

	log.Print(bannerLine)
	log.Print("FIRST RUN — copy below for your NEXT terminal session:")
	log.Print(bannerLine)
	log.Print("#   export PADDLE_API_KEY='pdl_sdbx_<your_key>'")
	log.Println()
	log.Printf("export %s=%s\n", envProductFixedFee, fixedFeeProdID)
	log.Printf("export %s=%s\n", envProductUsageFee, usageFeeProdID)
	log.Println()
	log.Println("# Optional: pin $0 recurring card-capture price")
	log.Printf("export %s=%s\n", envPriceCardCaptureZero, zeroPri)
	log.Println()
	log.Println("go run ./cmd/paddle-sandbox")
	log.Printf("\nProducts created (%d):\n", len(created))
	for _, cp := range created {
		log.Printf("  • %q → %s  (%s)\n", cp.DisplayName, cp.ID, cp.EnvKey)
	}
	log.Println(bannerLine)
}

func resolveZeroPrice(ctx context.Context, client *paddle.SDK, fixedFeeProductID string) (string, error) {
	if id := strings.TrimSpace(os.Getenv(envPriceCardCaptureZero)); id != "" {
		if _, err := client.GetPrice(ctx, &paddle.GetPriceRequest{PriceID: id}); err != nil {
			return "", fmt.Errorf("validate %s: %w", envPriceCardCaptureZero, err)
		}
		log.Printf("reuse card_capture_zero price_id=%s (from %s)", id, envPriceCardCaptureZero)
		return id, nil
	}
	if id, err := firstRecurringPriceID(ctx, client, fixedFeeProductID); err != nil {
		return "", err
	} else if id != "" {
		log.Printf("reuse card_capture price_id=%s (first recurring on %s)", id, productDisplayFixedFee)
		return id, nil
	}
	id, err := createZeroPlanPriceOnProduct(ctx, client, fixedFeeProductID)
	if err != nil {
		return "", err
	}
	log.Printf("created card_capture_zero price_id=%s on %s product", id, productDisplayFixedFee)
	return id, nil
}

func firstRecurringPriceID(ctx context.Context, client *paddle.SDK, productID string) (string, error) {
	pp := 50
	col, err := client.ListPrices(ctx, &paddle.ListPricesRequest{
		ProductID: []string{productID},
		Status:    []string{"active"},
		Recurring: boolPtr(true),
		PerPage:   &pp,
	})
	if err != nil {
		return "", err
	}
	if col == nil {
		return "", errors.New("unexpected nil price collection")
	}
	var id string
	if err := col.Iter(ctx, func(p *paddle.Price) (bool, error) {
		if p == nil {
			return true, nil
		}
		id = p.ID
		return false, nil
	}); err != nil {
		return "", err
	}
	return id, nil
}

func createZeroPlanPriceOnProduct(ctx context.Context, client *paddle.SDK, productID string) (string, error) {
	z, err := client.CreatePrice(ctx, &paddle.CreatePriceRequest{
		ProductID:   productID,
		Name:        paddle.PtrTo("Flexprice — $0 card capture"),
		Description: "Flexprice $0/month subscription price with trial requiring payment method at checkout.",
		UnitPrice:   paddle.Money{Amount: "0", CurrencyCode: paddle.CurrencyCodeUSD},
		BillingCycle: &paddle.Duration{
			Interval:  paddle.IntervalMonth,
			Frequency: 1,
		},
		TrialPeriod: &paddle.TrialPeriod{
			Interval:              paddle.IntervalMonth,
			Frequency:             1,
			RequiresPaymentMethod: true,
		},
	})
	if err != nil {
		return "", fmt.Errorf("CreatePrice zero plan: %w", err)
	}
	return z.ID, nil
}
