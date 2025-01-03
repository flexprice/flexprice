// Code generated by ent, DO NOT EDIT.

package ent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"reflect"

	"github.com/flexprice/flexprice/ent/migrate"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqlgraph"
	"github.com/flexprice/flexprice/ent/invoice"
	"github.com/flexprice/flexprice/ent/invoicelineitem"
	"github.com/flexprice/flexprice/ent/subscription"
	"github.com/flexprice/flexprice/ent/wallet"
	"github.com/flexprice/flexprice/ent/wallettransaction"
)

// Client is the client that holds all ent builders.
type Client struct {
	config
	// Schema is the client for creating, migrating and dropping schema.
	Schema *migrate.Schema
	// Invoice is the client for interacting with the Invoice builders.
	Invoice *InvoiceClient
	// InvoiceLineItem is the client for interacting with the InvoiceLineItem builders.
	InvoiceLineItem *InvoiceLineItemClient
	// Subscription is the client for interacting with the Subscription builders.
	Subscription *SubscriptionClient
	// Wallet is the client for interacting with the Wallet builders.
	Wallet *WalletClient
	// WalletTransaction is the client for interacting with the WalletTransaction builders.
	WalletTransaction *WalletTransactionClient
}

// NewClient creates a new client configured with the given options.
func NewClient(opts ...Option) *Client {
	client := &Client{config: newConfig(opts...)}
	client.init()
	return client
}

func (c *Client) init() {
	c.Schema = migrate.NewSchema(c.driver)
	c.Invoice = NewInvoiceClient(c.config)
	c.InvoiceLineItem = NewInvoiceLineItemClient(c.config)
	c.Subscription = NewSubscriptionClient(c.config)
	c.Wallet = NewWalletClient(c.config)
	c.WalletTransaction = NewWalletTransactionClient(c.config)
}

type (
	// config is the configuration for the client and its builder.
	config struct {
		// driver used for executing database requests.
		driver dialect.Driver
		// debug enable a debug logging.
		debug bool
		// log used for logging on debug mode.
		log func(...any)
		// hooks to execute on mutations.
		hooks *hooks
		// interceptors to execute on queries.
		inters *inters
	}
	// Option function to configure the client.
	Option func(*config)
)

// newConfig creates a new config for the client.
func newConfig(opts ...Option) config {
	cfg := config{log: log.Println, hooks: &hooks{}, inters: &inters{}}
	cfg.options(opts...)
	return cfg
}

// options applies the options on the config object.
func (c *config) options(opts ...Option) {
	for _, opt := range opts {
		opt(c)
	}
	if c.debug {
		c.driver = dialect.Debug(c.driver, c.log)
	}
}

// Debug enables debug logging on the ent.Driver.
func Debug() Option {
	return func(c *config) {
		c.debug = true
	}
}

// Log sets the logging function for debug mode.
func Log(fn func(...any)) Option {
	return func(c *config) {
		c.log = fn
	}
}

// Driver configures the client driver.
func Driver(driver dialect.Driver) Option {
	return func(c *config) {
		c.driver = driver
	}
}

// Open opens a database/sql.DB specified by the driver name and
// the data source name, and returns a new client attached to it.
// Optional parameters can be added for configuring the client.
func Open(driverName, dataSourceName string, options ...Option) (*Client, error) {
	switch driverName {
	case dialect.MySQL, dialect.Postgres, dialect.SQLite:
		drv, err := sql.Open(driverName, dataSourceName)
		if err != nil {
			return nil, err
		}
		return NewClient(append(options, Driver(drv))...), nil
	default:
		return nil, fmt.Errorf("unsupported driver: %q", driverName)
	}
}

// ErrTxStarted is returned when trying to start a new transaction from a transactional client.
var ErrTxStarted = errors.New("ent: cannot start a transaction within a transaction")

// Tx returns a new transactional client. The provided context
// is used until the transaction is committed or rolled back.
func (c *Client) Tx(ctx context.Context) (*Tx, error) {
	if _, ok := c.driver.(*txDriver); ok {
		return nil, ErrTxStarted
	}
	tx, err := newTx(ctx, c.driver)
	if err != nil {
		return nil, fmt.Errorf("ent: starting a transaction: %w", err)
	}
	cfg := c.config
	cfg.driver = tx
	return &Tx{
		ctx:               ctx,
		config:            cfg,
		Invoice:           NewInvoiceClient(cfg),
		InvoiceLineItem:   NewInvoiceLineItemClient(cfg),
		Subscription:      NewSubscriptionClient(cfg),
		Wallet:            NewWalletClient(cfg),
		WalletTransaction: NewWalletTransactionClient(cfg),
	}, nil
}

// BeginTx returns a transactional client with specified options.
func (c *Client) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	if _, ok := c.driver.(*txDriver); ok {
		return nil, errors.New("ent: cannot start a transaction within a transaction")
	}
	tx, err := c.driver.(interface {
		BeginTx(context.Context, *sql.TxOptions) (dialect.Tx, error)
	}).BeginTx(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("ent: starting a transaction: %w", err)
	}
	cfg := c.config
	cfg.driver = &txDriver{tx: tx, drv: c.driver}
	return &Tx{
		ctx:               ctx,
		config:            cfg,
		Invoice:           NewInvoiceClient(cfg),
		InvoiceLineItem:   NewInvoiceLineItemClient(cfg),
		Subscription:      NewSubscriptionClient(cfg),
		Wallet:            NewWalletClient(cfg),
		WalletTransaction: NewWalletTransactionClient(cfg),
	}, nil
}

// Debug returns a new debug-client. It's used to get verbose logging on specific operations.
//
//	client.Debug().
//		Invoice.
//		Query().
//		Count(ctx)
func (c *Client) Debug() *Client {
	if c.debug {
		return c
	}
	cfg := c.config
	cfg.driver = dialect.Debug(c.driver, c.log)
	client := &Client{config: cfg}
	client.init()
	return client
}

// Close closes the database connection and prevents new queries from starting.
func (c *Client) Close() error {
	return c.driver.Close()
}

// Use adds the mutation hooks to all the entity clients.
// In order to add hooks to a specific client, call: `client.Node.Use(...)`.
func (c *Client) Use(hooks ...Hook) {
	c.Invoice.Use(hooks...)
	c.InvoiceLineItem.Use(hooks...)
	c.Subscription.Use(hooks...)
	c.Wallet.Use(hooks...)
	c.WalletTransaction.Use(hooks...)
}

// Intercept adds the query interceptors to all the entity clients.
// In order to add interceptors to a specific client, call: `client.Node.Intercept(...)`.
func (c *Client) Intercept(interceptors ...Interceptor) {
	c.Invoice.Intercept(interceptors...)
	c.InvoiceLineItem.Intercept(interceptors...)
	c.Subscription.Intercept(interceptors...)
	c.Wallet.Intercept(interceptors...)
	c.WalletTransaction.Intercept(interceptors...)
}

// Mutate implements the ent.Mutator interface.
func (c *Client) Mutate(ctx context.Context, m Mutation) (Value, error) {
	switch m := m.(type) {
	case *InvoiceMutation:
		return c.Invoice.mutate(ctx, m)
	case *InvoiceLineItemMutation:
		return c.InvoiceLineItem.mutate(ctx, m)
	case *SubscriptionMutation:
		return c.Subscription.mutate(ctx, m)
	case *WalletMutation:
		return c.Wallet.mutate(ctx, m)
	case *WalletTransactionMutation:
		return c.WalletTransaction.mutate(ctx, m)
	default:
		return nil, fmt.Errorf("ent: unknown mutation type %T", m)
	}
}

// InvoiceClient is a client for the Invoice schema.
type InvoiceClient struct {
	config
}

// NewInvoiceClient returns a client for the Invoice from the given config.
func NewInvoiceClient(c config) *InvoiceClient {
	return &InvoiceClient{config: c}
}

// Use adds a list of mutation hooks to the hooks stack.
// A call to `Use(f, g, h)` equals to `invoice.Hooks(f(g(h())))`.
func (c *InvoiceClient) Use(hooks ...Hook) {
	c.hooks.Invoice = append(c.hooks.Invoice, hooks...)
}

// Intercept adds a list of query interceptors to the interceptors stack.
// A call to `Intercept(f, g, h)` equals to `invoice.Intercept(f(g(h())))`.
func (c *InvoiceClient) Intercept(interceptors ...Interceptor) {
	c.inters.Invoice = append(c.inters.Invoice, interceptors...)
}

// Create returns a builder for creating a Invoice entity.
func (c *InvoiceClient) Create() *InvoiceCreate {
	mutation := newInvoiceMutation(c.config, OpCreate)
	return &InvoiceCreate{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// CreateBulk returns a builder for creating a bulk of Invoice entities.
func (c *InvoiceClient) CreateBulk(builders ...*InvoiceCreate) *InvoiceCreateBulk {
	return &InvoiceCreateBulk{config: c.config, builders: builders}
}

// MapCreateBulk creates a bulk creation builder from the given slice. For each item in the slice, the function creates
// a builder and applies setFunc on it.
func (c *InvoiceClient) MapCreateBulk(slice any, setFunc func(*InvoiceCreate, int)) *InvoiceCreateBulk {
	rv := reflect.ValueOf(slice)
	if rv.Kind() != reflect.Slice {
		return &InvoiceCreateBulk{err: fmt.Errorf("calling to InvoiceClient.MapCreateBulk with wrong type %T, need slice", slice)}
	}
	builders := make([]*InvoiceCreate, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		builders[i] = c.Create()
		setFunc(builders[i], i)
	}
	return &InvoiceCreateBulk{config: c.config, builders: builders}
}

// Update returns an update builder for Invoice.
func (c *InvoiceClient) Update() *InvoiceUpdate {
	mutation := newInvoiceMutation(c.config, OpUpdate)
	return &InvoiceUpdate{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// UpdateOne returns an update builder for the given entity.
func (c *InvoiceClient) UpdateOne(i *Invoice) *InvoiceUpdateOne {
	mutation := newInvoiceMutation(c.config, OpUpdateOne, withInvoice(i))
	return &InvoiceUpdateOne{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// UpdateOneID returns an update builder for the given id.
func (c *InvoiceClient) UpdateOneID(id string) *InvoiceUpdateOne {
	mutation := newInvoiceMutation(c.config, OpUpdateOne, withInvoiceID(id))
	return &InvoiceUpdateOne{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// Delete returns a delete builder for Invoice.
func (c *InvoiceClient) Delete() *InvoiceDelete {
	mutation := newInvoiceMutation(c.config, OpDelete)
	return &InvoiceDelete{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// DeleteOne returns a builder for deleting the given entity.
func (c *InvoiceClient) DeleteOne(i *Invoice) *InvoiceDeleteOne {
	return c.DeleteOneID(i.ID)
}

// DeleteOneID returns a builder for deleting the given entity by its id.
func (c *InvoiceClient) DeleteOneID(id string) *InvoiceDeleteOne {
	builder := c.Delete().Where(invoice.ID(id))
	builder.mutation.id = &id
	builder.mutation.op = OpDeleteOne
	return &InvoiceDeleteOne{builder}
}

// Query returns a query builder for Invoice.
func (c *InvoiceClient) Query() *InvoiceQuery {
	return &InvoiceQuery{
		config: c.config,
		ctx:    &QueryContext{Type: TypeInvoice},
		inters: c.Interceptors(),
	}
}

// Get returns a Invoice entity by its id.
func (c *InvoiceClient) Get(ctx context.Context, id string) (*Invoice, error) {
	return c.Query().Where(invoice.ID(id)).Only(ctx)
}

// GetX is like Get, but panics if an error occurs.
func (c *InvoiceClient) GetX(ctx context.Context, id string) *Invoice {
	obj, err := c.Get(ctx, id)
	if err != nil {
		panic(err)
	}
	return obj
}

// QueryLineItems queries the line_items edge of a Invoice.
func (c *InvoiceClient) QueryLineItems(i *Invoice) *InvoiceLineItemQuery {
	query := (&InvoiceLineItemClient{config: c.config}).Query()
	query.path = func(context.Context) (fromV *sql.Selector, _ error) {
		id := i.ID
		step := sqlgraph.NewStep(
			sqlgraph.From(invoice.Table, invoice.FieldID, id),
			sqlgraph.To(invoicelineitem.Table, invoicelineitem.FieldID),
			sqlgraph.Edge(sqlgraph.O2M, false, invoice.LineItemsTable, invoice.LineItemsColumn),
		)
		fromV = sqlgraph.Neighbors(i.driver.Dialect(), step)
		return fromV, nil
	}
	return query
}

// Hooks returns the client hooks.
func (c *InvoiceClient) Hooks() []Hook {
	return c.hooks.Invoice
}

// Interceptors returns the client interceptors.
func (c *InvoiceClient) Interceptors() []Interceptor {
	return c.inters.Invoice
}

func (c *InvoiceClient) mutate(ctx context.Context, m *InvoiceMutation) (Value, error) {
	switch m.Op() {
	case OpCreate:
		return (&InvoiceCreate{config: c.config, hooks: c.Hooks(), mutation: m}).Save(ctx)
	case OpUpdate:
		return (&InvoiceUpdate{config: c.config, hooks: c.Hooks(), mutation: m}).Save(ctx)
	case OpUpdateOne:
		return (&InvoiceUpdateOne{config: c.config, hooks: c.Hooks(), mutation: m}).Save(ctx)
	case OpDelete, OpDeleteOne:
		return (&InvoiceDelete{config: c.config, hooks: c.Hooks(), mutation: m}).Exec(ctx)
	default:
		return nil, fmt.Errorf("ent: unknown Invoice mutation op: %q", m.Op())
	}
}

// InvoiceLineItemClient is a client for the InvoiceLineItem schema.
type InvoiceLineItemClient struct {
	config
}

// NewInvoiceLineItemClient returns a client for the InvoiceLineItem from the given config.
func NewInvoiceLineItemClient(c config) *InvoiceLineItemClient {
	return &InvoiceLineItemClient{config: c}
}

// Use adds a list of mutation hooks to the hooks stack.
// A call to `Use(f, g, h)` equals to `invoicelineitem.Hooks(f(g(h())))`.
func (c *InvoiceLineItemClient) Use(hooks ...Hook) {
	c.hooks.InvoiceLineItem = append(c.hooks.InvoiceLineItem, hooks...)
}

// Intercept adds a list of query interceptors to the interceptors stack.
// A call to `Intercept(f, g, h)` equals to `invoicelineitem.Intercept(f(g(h())))`.
func (c *InvoiceLineItemClient) Intercept(interceptors ...Interceptor) {
	c.inters.InvoiceLineItem = append(c.inters.InvoiceLineItem, interceptors...)
}

// Create returns a builder for creating a InvoiceLineItem entity.
func (c *InvoiceLineItemClient) Create() *InvoiceLineItemCreate {
	mutation := newInvoiceLineItemMutation(c.config, OpCreate)
	return &InvoiceLineItemCreate{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// CreateBulk returns a builder for creating a bulk of InvoiceLineItem entities.
func (c *InvoiceLineItemClient) CreateBulk(builders ...*InvoiceLineItemCreate) *InvoiceLineItemCreateBulk {
	return &InvoiceLineItemCreateBulk{config: c.config, builders: builders}
}

// MapCreateBulk creates a bulk creation builder from the given slice. For each item in the slice, the function creates
// a builder and applies setFunc on it.
func (c *InvoiceLineItemClient) MapCreateBulk(slice any, setFunc func(*InvoiceLineItemCreate, int)) *InvoiceLineItemCreateBulk {
	rv := reflect.ValueOf(slice)
	if rv.Kind() != reflect.Slice {
		return &InvoiceLineItemCreateBulk{err: fmt.Errorf("calling to InvoiceLineItemClient.MapCreateBulk with wrong type %T, need slice", slice)}
	}
	builders := make([]*InvoiceLineItemCreate, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		builders[i] = c.Create()
		setFunc(builders[i], i)
	}
	return &InvoiceLineItemCreateBulk{config: c.config, builders: builders}
}

// Update returns an update builder for InvoiceLineItem.
func (c *InvoiceLineItemClient) Update() *InvoiceLineItemUpdate {
	mutation := newInvoiceLineItemMutation(c.config, OpUpdate)
	return &InvoiceLineItemUpdate{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// UpdateOne returns an update builder for the given entity.
func (c *InvoiceLineItemClient) UpdateOne(ili *InvoiceLineItem) *InvoiceLineItemUpdateOne {
	mutation := newInvoiceLineItemMutation(c.config, OpUpdateOne, withInvoiceLineItem(ili))
	return &InvoiceLineItemUpdateOne{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// UpdateOneID returns an update builder for the given id.
func (c *InvoiceLineItemClient) UpdateOneID(id string) *InvoiceLineItemUpdateOne {
	mutation := newInvoiceLineItemMutation(c.config, OpUpdateOne, withInvoiceLineItemID(id))
	return &InvoiceLineItemUpdateOne{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// Delete returns a delete builder for InvoiceLineItem.
func (c *InvoiceLineItemClient) Delete() *InvoiceLineItemDelete {
	mutation := newInvoiceLineItemMutation(c.config, OpDelete)
	return &InvoiceLineItemDelete{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// DeleteOne returns a builder for deleting the given entity.
func (c *InvoiceLineItemClient) DeleteOne(ili *InvoiceLineItem) *InvoiceLineItemDeleteOne {
	return c.DeleteOneID(ili.ID)
}

// DeleteOneID returns a builder for deleting the given entity by its id.
func (c *InvoiceLineItemClient) DeleteOneID(id string) *InvoiceLineItemDeleteOne {
	builder := c.Delete().Where(invoicelineitem.ID(id))
	builder.mutation.id = &id
	builder.mutation.op = OpDeleteOne
	return &InvoiceLineItemDeleteOne{builder}
}

// Query returns a query builder for InvoiceLineItem.
func (c *InvoiceLineItemClient) Query() *InvoiceLineItemQuery {
	return &InvoiceLineItemQuery{
		config: c.config,
		ctx:    &QueryContext{Type: TypeInvoiceLineItem},
		inters: c.Interceptors(),
	}
}

// Get returns a InvoiceLineItem entity by its id.
func (c *InvoiceLineItemClient) Get(ctx context.Context, id string) (*InvoiceLineItem, error) {
	return c.Query().Where(invoicelineitem.ID(id)).Only(ctx)
}

// GetX is like Get, but panics if an error occurs.
func (c *InvoiceLineItemClient) GetX(ctx context.Context, id string) *InvoiceLineItem {
	obj, err := c.Get(ctx, id)
	if err != nil {
		panic(err)
	}
	return obj
}

// QueryInvoice queries the invoice edge of a InvoiceLineItem.
func (c *InvoiceLineItemClient) QueryInvoice(ili *InvoiceLineItem) *InvoiceQuery {
	query := (&InvoiceClient{config: c.config}).Query()
	query.path = func(context.Context) (fromV *sql.Selector, _ error) {
		id := ili.ID
		step := sqlgraph.NewStep(
			sqlgraph.From(invoicelineitem.Table, invoicelineitem.FieldID, id),
			sqlgraph.To(invoice.Table, invoice.FieldID),
			sqlgraph.Edge(sqlgraph.M2O, true, invoicelineitem.InvoiceTable, invoicelineitem.InvoiceColumn),
		)
		fromV = sqlgraph.Neighbors(ili.driver.Dialect(), step)
		return fromV, nil
	}
	return query
}

// Hooks returns the client hooks.
func (c *InvoiceLineItemClient) Hooks() []Hook {
	return c.hooks.InvoiceLineItem
}

// Interceptors returns the client interceptors.
func (c *InvoiceLineItemClient) Interceptors() []Interceptor {
	return c.inters.InvoiceLineItem
}

func (c *InvoiceLineItemClient) mutate(ctx context.Context, m *InvoiceLineItemMutation) (Value, error) {
	switch m.Op() {
	case OpCreate:
		return (&InvoiceLineItemCreate{config: c.config, hooks: c.Hooks(), mutation: m}).Save(ctx)
	case OpUpdate:
		return (&InvoiceLineItemUpdate{config: c.config, hooks: c.Hooks(), mutation: m}).Save(ctx)
	case OpUpdateOne:
		return (&InvoiceLineItemUpdateOne{config: c.config, hooks: c.Hooks(), mutation: m}).Save(ctx)
	case OpDelete, OpDeleteOne:
		return (&InvoiceLineItemDelete{config: c.config, hooks: c.Hooks(), mutation: m}).Exec(ctx)
	default:
		return nil, fmt.Errorf("ent: unknown InvoiceLineItem mutation op: %q", m.Op())
	}
}

// SubscriptionClient is a client for the Subscription schema.
type SubscriptionClient struct {
	config
}

// NewSubscriptionClient returns a client for the Subscription from the given config.
func NewSubscriptionClient(c config) *SubscriptionClient {
	return &SubscriptionClient{config: c}
}

// Use adds a list of mutation hooks to the hooks stack.
// A call to `Use(f, g, h)` equals to `subscription.Hooks(f(g(h())))`.
func (c *SubscriptionClient) Use(hooks ...Hook) {
	c.hooks.Subscription = append(c.hooks.Subscription, hooks...)
}

// Intercept adds a list of query interceptors to the interceptors stack.
// A call to `Intercept(f, g, h)` equals to `subscription.Intercept(f(g(h())))`.
func (c *SubscriptionClient) Intercept(interceptors ...Interceptor) {
	c.inters.Subscription = append(c.inters.Subscription, interceptors...)
}

// Create returns a builder for creating a Subscription entity.
func (c *SubscriptionClient) Create() *SubscriptionCreate {
	mutation := newSubscriptionMutation(c.config, OpCreate)
	return &SubscriptionCreate{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// CreateBulk returns a builder for creating a bulk of Subscription entities.
func (c *SubscriptionClient) CreateBulk(builders ...*SubscriptionCreate) *SubscriptionCreateBulk {
	return &SubscriptionCreateBulk{config: c.config, builders: builders}
}

// MapCreateBulk creates a bulk creation builder from the given slice. For each item in the slice, the function creates
// a builder and applies setFunc on it.
func (c *SubscriptionClient) MapCreateBulk(slice any, setFunc func(*SubscriptionCreate, int)) *SubscriptionCreateBulk {
	rv := reflect.ValueOf(slice)
	if rv.Kind() != reflect.Slice {
		return &SubscriptionCreateBulk{err: fmt.Errorf("calling to SubscriptionClient.MapCreateBulk with wrong type %T, need slice", slice)}
	}
	builders := make([]*SubscriptionCreate, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		builders[i] = c.Create()
		setFunc(builders[i], i)
	}
	return &SubscriptionCreateBulk{config: c.config, builders: builders}
}

// Update returns an update builder for Subscription.
func (c *SubscriptionClient) Update() *SubscriptionUpdate {
	mutation := newSubscriptionMutation(c.config, OpUpdate)
	return &SubscriptionUpdate{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// UpdateOne returns an update builder for the given entity.
func (c *SubscriptionClient) UpdateOne(s *Subscription) *SubscriptionUpdateOne {
	mutation := newSubscriptionMutation(c.config, OpUpdateOne, withSubscription(s))
	return &SubscriptionUpdateOne{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// UpdateOneID returns an update builder for the given id.
func (c *SubscriptionClient) UpdateOneID(id string) *SubscriptionUpdateOne {
	mutation := newSubscriptionMutation(c.config, OpUpdateOne, withSubscriptionID(id))
	return &SubscriptionUpdateOne{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// Delete returns a delete builder for Subscription.
func (c *SubscriptionClient) Delete() *SubscriptionDelete {
	mutation := newSubscriptionMutation(c.config, OpDelete)
	return &SubscriptionDelete{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// DeleteOne returns a builder for deleting the given entity.
func (c *SubscriptionClient) DeleteOne(s *Subscription) *SubscriptionDeleteOne {
	return c.DeleteOneID(s.ID)
}

// DeleteOneID returns a builder for deleting the given entity by its id.
func (c *SubscriptionClient) DeleteOneID(id string) *SubscriptionDeleteOne {
	builder := c.Delete().Where(subscription.ID(id))
	builder.mutation.id = &id
	builder.mutation.op = OpDeleteOne
	return &SubscriptionDeleteOne{builder}
}

// Query returns a query builder for Subscription.
func (c *SubscriptionClient) Query() *SubscriptionQuery {
	return &SubscriptionQuery{
		config: c.config,
		ctx:    &QueryContext{Type: TypeSubscription},
		inters: c.Interceptors(),
	}
}

// Get returns a Subscription entity by its id.
func (c *SubscriptionClient) Get(ctx context.Context, id string) (*Subscription, error) {
	return c.Query().Where(subscription.ID(id)).Only(ctx)
}

// GetX is like Get, but panics if an error occurs.
func (c *SubscriptionClient) GetX(ctx context.Context, id string) *Subscription {
	obj, err := c.Get(ctx, id)
	if err != nil {
		panic(err)
	}
	return obj
}

// Hooks returns the client hooks.
func (c *SubscriptionClient) Hooks() []Hook {
	return c.hooks.Subscription
}

// Interceptors returns the client interceptors.
func (c *SubscriptionClient) Interceptors() []Interceptor {
	return c.inters.Subscription
}

func (c *SubscriptionClient) mutate(ctx context.Context, m *SubscriptionMutation) (Value, error) {
	switch m.Op() {
	case OpCreate:
		return (&SubscriptionCreate{config: c.config, hooks: c.Hooks(), mutation: m}).Save(ctx)
	case OpUpdate:
		return (&SubscriptionUpdate{config: c.config, hooks: c.Hooks(), mutation: m}).Save(ctx)
	case OpUpdateOne:
		return (&SubscriptionUpdateOne{config: c.config, hooks: c.Hooks(), mutation: m}).Save(ctx)
	case OpDelete, OpDeleteOne:
		return (&SubscriptionDelete{config: c.config, hooks: c.Hooks(), mutation: m}).Exec(ctx)
	default:
		return nil, fmt.Errorf("ent: unknown Subscription mutation op: %q", m.Op())
	}
}

// WalletClient is a client for the Wallet schema.
type WalletClient struct {
	config
}

// NewWalletClient returns a client for the Wallet from the given config.
func NewWalletClient(c config) *WalletClient {
	return &WalletClient{config: c}
}

// Use adds a list of mutation hooks to the hooks stack.
// A call to `Use(f, g, h)` equals to `wallet.Hooks(f(g(h())))`.
func (c *WalletClient) Use(hooks ...Hook) {
	c.hooks.Wallet = append(c.hooks.Wallet, hooks...)
}

// Intercept adds a list of query interceptors to the interceptors stack.
// A call to `Intercept(f, g, h)` equals to `wallet.Intercept(f(g(h())))`.
func (c *WalletClient) Intercept(interceptors ...Interceptor) {
	c.inters.Wallet = append(c.inters.Wallet, interceptors...)
}

// Create returns a builder for creating a Wallet entity.
func (c *WalletClient) Create() *WalletCreate {
	mutation := newWalletMutation(c.config, OpCreate)
	return &WalletCreate{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// CreateBulk returns a builder for creating a bulk of Wallet entities.
func (c *WalletClient) CreateBulk(builders ...*WalletCreate) *WalletCreateBulk {
	return &WalletCreateBulk{config: c.config, builders: builders}
}

// MapCreateBulk creates a bulk creation builder from the given slice. For each item in the slice, the function creates
// a builder and applies setFunc on it.
func (c *WalletClient) MapCreateBulk(slice any, setFunc func(*WalletCreate, int)) *WalletCreateBulk {
	rv := reflect.ValueOf(slice)
	if rv.Kind() != reflect.Slice {
		return &WalletCreateBulk{err: fmt.Errorf("calling to WalletClient.MapCreateBulk with wrong type %T, need slice", slice)}
	}
	builders := make([]*WalletCreate, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		builders[i] = c.Create()
		setFunc(builders[i], i)
	}
	return &WalletCreateBulk{config: c.config, builders: builders}
}

// Update returns an update builder for Wallet.
func (c *WalletClient) Update() *WalletUpdate {
	mutation := newWalletMutation(c.config, OpUpdate)
	return &WalletUpdate{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// UpdateOne returns an update builder for the given entity.
func (c *WalletClient) UpdateOne(w *Wallet) *WalletUpdateOne {
	mutation := newWalletMutation(c.config, OpUpdateOne, withWallet(w))
	return &WalletUpdateOne{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// UpdateOneID returns an update builder for the given id.
func (c *WalletClient) UpdateOneID(id string) *WalletUpdateOne {
	mutation := newWalletMutation(c.config, OpUpdateOne, withWalletID(id))
	return &WalletUpdateOne{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// Delete returns a delete builder for Wallet.
func (c *WalletClient) Delete() *WalletDelete {
	mutation := newWalletMutation(c.config, OpDelete)
	return &WalletDelete{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// DeleteOne returns a builder for deleting the given entity.
func (c *WalletClient) DeleteOne(w *Wallet) *WalletDeleteOne {
	return c.DeleteOneID(w.ID)
}

// DeleteOneID returns a builder for deleting the given entity by its id.
func (c *WalletClient) DeleteOneID(id string) *WalletDeleteOne {
	builder := c.Delete().Where(wallet.ID(id))
	builder.mutation.id = &id
	builder.mutation.op = OpDeleteOne
	return &WalletDeleteOne{builder}
}

// Query returns a query builder for Wallet.
func (c *WalletClient) Query() *WalletQuery {
	return &WalletQuery{
		config: c.config,
		ctx:    &QueryContext{Type: TypeWallet},
		inters: c.Interceptors(),
	}
}

// Get returns a Wallet entity by its id.
func (c *WalletClient) Get(ctx context.Context, id string) (*Wallet, error) {
	return c.Query().Where(wallet.ID(id)).Only(ctx)
}

// GetX is like Get, but panics if an error occurs.
func (c *WalletClient) GetX(ctx context.Context, id string) *Wallet {
	obj, err := c.Get(ctx, id)
	if err != nil {
		panic(err)
	}
	return obj
}

// Hooks returns the client hooks.
func (c *WalletClient) Hooks() []Hook {
	return c.hooks.Wallet
}

// Interceptors returns the client interceptors.
func (c *WalletClient) Interceptors() []Interceptor {
	return c.inters.Wallet
}

func (c *WalletClient) mutate(ctx context.Context, m *WalletMutation) (Value, error) {
	switch m.Op() {
	case OpCreate:
		return (&WalletCreate{config: c.config, hooks: c.Hooks(), mutation: m}).Save(ctx)
	case OpUpdate:
		return (&WalletUpdate{config: c.config, hooks: c.Hooks(), mutation: m}).Save(ctx)
	case OpUpdateOne:
		return (&WalletUpdateOne{config: c.config, hooks: c.Hooks(), mutation: m}).Save(ctx)
	case OpDelete, OpDeleteOne:
		return (&WalletDelete{config: c.config, hooks: c.Hooks(), mutation: m}).Exec(ctx)
	default:
		return nil, fmt.Errorf("ent: unknown Wallet mutation op: %q", m.Op())
	}
}

// WalletTransactionClient is a client for the WalletTransaction schema.
type WalletTransactionClient struct {
	config
}

// NewWalletTransactionClient returns a client for the WalletTransaction from the given config.
func NewWalletTransactionClient(c config) *WalletTransactionClient {
	return &WalletTransactionClient{config: c}
}

// Use adds a list of mutation hooks to the hooks stack.
// A call to `Use(f, g, h)` equals to `wallettransaction.Hooks(f(g(h())))`.
func (c *WalletTransactionClient) Use(hooks ...Hook) {
	c.hooks.WalletTransaction = append(c.hooks.WalletTransaction, hooks...)
}

// Intercept adds a list of query interceptors to the interceptors stack.
// A call to `Intercept(f, g, h)` equals to `wallettransaction.Intercept(f(g(h())))`.
func (c *WalletTransactionClient) Intercept(interceptors ...Interceptor) {
	c.inters.WalletTransaction = append(c.inters.WalletTransaction, interceptors...)
}

// Create returns a builder for creating a WalletTransaction entity.
func (c *WalletTransactionClient) Create() *WalletTransactionCreate {
	mutation := newWalletTransactionMutation(c.config, OpCreate)
	return &WalletTransactionCreate{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// CreateBulk returns a builder for creating a bulk of WalletTransaction entities.
func (c *WalletTransactionClient) CreateBulk(builders ...*WalletTransactionCreate) *WalletTransactionCreateBulk {
	return &WalletTransactionCreateBulk{config: c.config, builders: builders}
}

// MapCreateBulk creates a bulk creation builder from the given slice. For each item in the slice, the function creates
// a builder and applies setFunc on it.
func (c *WalletTransactionClient) MapCreateBulk(slice any, setFunc func(*WalletTransactionCreate, int)) *WalletTransactionCreateBulk {
	rv := reflect.ValueOf(slice)
	if rv.Kind() != reflect.Slice {
		return &WalletTransactionCreateBulk{err: fmt.Errorf("calling to WalletTransactionClient.MapCreateBulk with wrong type %T, need slice", slice)}
	}
	builders := make([]*WalletTransactionCreate, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		builders[i] = c.Create()
		setFunc(builders[i], i)
	}
	return &WalletTransactionCreateBulk{config: c.config, builders: builders}
}

// Update returns an update builder for WalletTransaction.
func (c *WalletTransactionClient) Update() *WalletTransactionUpdate {
	mutation := newWalletTransactionMutation(c.config, OpUpdate)
	return &WalletTransactionUpdate{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// UpdateOne returns an update builder for the given entity.
func (c *WalletTransactionClient) UpdateOne(wt *WalletTransaction) *WalletTransactionUpdateOne {
	mutation := newWalletTransactionMutation(c.config, OpUpdateOne, withWalletTransaction(wt))
	return &WalletTransactionUpdateOne{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// UpdateOneID returns an update builder for the given id.
func (c *WalletTransactionClient) UpdateOneID(id string) *WalletTransactionUpdateOne {
	mutation := newWalletTransactionMutation(c.config, OpUpdateOne, withWalletTransactionID(id))
	return &WalletTransactionUpdateOne{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// Delete returns a delete builder for WalletTransaction.
func (c *WalletTransactionClient) Delete() *WalletTransactionDelete {
	mutation := newWalletTransactionMutation(c.config, OpDelete)
	return &WalletTransactionDelete{config: c.config, hooks: c.Hooks(), mutation: mutation}
}

// DeleteOne returns a builder for deleting the given entity.
func (c *WalletTransactionClient) DeleteOne(wt *WalletTransaction) *WalletTransactionDeleteOne {
	return c.DeleteOneID(wt.ID)
}

// DeleteOneID returns a builder for deleting the given entity by its id.
func (c *WalletTransactionClient) DeleteOneID(id string) *WalletTransactionDeleteOne {
	builder := c.Delete().Where(wallettransaction.ID(id))
	builder.mutation.id = &id
	builder.mutation.op = OpDeleteOne
	return &WalletTransactionDeleteOne{builder}
}

// Query returns a query builder for WalletTransaction.
func (c *WalletTransactionClient) Query() *WalletTransactionQuery {
	return &WalletTransactionQuery{
		config: c.config,
		ctx:    &QueryContext{Type: TypeWalletTransaction},
		inters: c.Interceptors(),
	}
}

// Get returns a WalletTransaction entity by its id.
func (c *WalletTransactionClient) Get(ctx context.Context, id string) (*WalletTransaction, error) {
	return c.Query().Where(wallettransaction.ID(id)).Only(ctx)
}

// GetX is like Get, but panics if an error occurs.
func (c *WalletTransactionClient) GetX(ctx context.Context, id string) *WalletTransaction {
	obj, err := c.Get(ctx, id)
	if err != nil {
		panic(err)
	}
	return obj
}

// Hooks returns the client hooks.
func (c *WalletTransactionClient) Hooks() []Hook {
	return c.hooks.WalletTransaction
}

// Interceptors returns the client interceptors.
func (c *WalletTransactionClient) Interceptors() []Interceptor {
	return c.inters.WalletTransaction
}

func (c *WalletTransactionClient) mutate(ctx context.Context, m *WalletTransactionMutation) (Value, error) {
	switch m.Op() {
	case OpCreate:
		return (&WalletTransactionCreate{config: c.config, hooks: c.Hooks(), mutation: m}).Save(ctx)
	case OpUpdate:
		return (&WalletTransactionUpdate{config: c.config, hooks: c.Hooks(), mutation: m}).Save(ctx)
	case OpUpdateOne:
		return (&WalletTransactionUpdateOne{config: c.config, hooks: c.Hooks(), mutation: m}).Save(ctx)
	case OpDelete, OpDeleteOne:
		return (&WalletTransactionDelete{config: c.config, hooks: c.Hooks(), mutation: m}).Exec(ctx)
	default:
		return nil, fmt.Errorf("ent: unknown WalletTransaction mutation op: %q", m.Op())
	}
}

// hooks and interceptors per client, for fast access.
type (
	hooks struct {
		Invoice, InvoiceLineItem, Subscription, Wallet, WalletTransaction []ent.Hook
	}
	inters struct {
		Invoice, InvoiceLineItem, Subscription, Wallet,
		WalletTransaction []ent.Interceptor
	}
)
