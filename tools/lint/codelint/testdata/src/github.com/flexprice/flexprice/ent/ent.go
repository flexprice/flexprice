package ent

import "context"

type Customer struct{}

type CustomerUpdate struct{}

func (u *CustomerUpdate) Save(context.Context) (int, error)                  { return 0, nil }
func (u *CustomerUpdate) Exec(context.Context) error                         { return nil }
func (u *CustomerUpdate) SaveReturning(context.Context) ([]*Customer, error) { return nil, nil }

type CustomerUpdateOne struct{}

func (u *CustomerUpdateOne) Save(context.Context) (*Customer, error) { return nil, nil }

type CustomerDelete struct{}

func (d *CustomerDelete) Exec(context.Context) (int, error)                  { return 0, nil }
func (d *CustomerDelete) ExecReturning(context.Context) ([]*Customer, error) { return nil, nil }

type EventUpdate struct{}

func (u *EventUpdate) Save(context.Context) (int, error) { return 0, nil }

type Client struct{}

func (c *Client) UpdateCustomers() *CustomerUpdate   { return &CustomerUpdate{} }
func (c *Client) UpdateCustomer() *CustomerUpdateOne { return &CustomerUpdateOne{} }
func (c *Client) DeleteCustomers() *CustomerDelete   { return &CustomerDelete{} }
func (c *Client) UpdateEvents() *EventUpdate         { return &EventUpdate{} }
