package doldb

import (
	"database/sql"
	"time"
)

// SearchResult is a compact row for listing results
type SearchResult struct {
	ID           int64          `json:"id"`
	Ref          string         `json:"ref"`
	Label        string         `json:"label,omitempty"`
	Customer     string         `json:"customer,omitempty"`
	CustomerID   int64          `json:"customer_id,omitempty"`
	Status       string         `json:"status"`
	StatusCode   int            `json:"status_code"`
	TotalHT      float64        `json:"total_ht,omitempty"`
	TotalTTC     float64        `json:"total_ttc,omitempty"`
	Currency     string         `json:"currency,omitempty"`
	Date         *time.Time     `json:"date,omitempty"`
	DateEnd      *time.Time     `json:"date_end,omitempty"`
}

// Proposal (propal)
type Proposal struct {
	ID                  int64      `json:"id"`
	Ref                 string     `json:"ref"`
	RefClient           string     `json:"ref_client,omitempty"`
	Entity              int        `json:"entity"`
	CustomerID          int64      `json:"customer_id"`
	CustomerName        string     `json:"customer_name,omitempty"`
	CustomerCode        string     `json:"customer_code,omitempty"`
	ProjectID           *int64     `json:"project_id,omitempty"`
	Status              int        `json:"status_code"`
	StatusLabel         string     `json:"status"`
	Date                *time.Time `json:"date,omitempty"`
	DateEnd             *time.Time `json:"validity_end,omitempty"`
	DeliveryDate        *time.Time `json:"delivery_date,omitempty"`
	TotalHT             float64    `json:"total_ht"`
	TotalVAT            float64    `json:"total_vat"`
	TotalTTC            float64    `json:"total_ttc"`
	Currency            string     `json:"currency"`
	MultiCurrencyCode   string     `json:"multi_currency_code,omitempty"`
	MultiCurrencyTTC    float64    `json:"multi_currency_ttc,omitempty"`
	PaymentTermLabel    string     `json:"payment_term,omitempty"`
	PaymentModeLabel    string     `json:"payment_mode,omitempty"`
	AvailabilityLabel   string     `json:"availability,omitempty"`
	ShippingMethodLabel string     `json:"shipping_method,omitempty"`
	SourceLabel         string     `json:"source,omitempty"`
	IncotermLabel       string     `json:"incoterms,omitempty"`
	IncotermLocation    string     `json:"incoterms_location,omitempty"`
	NotePublic          string     `json:"note_public,omitempty"`
	NotePrivate         string     `json:"note_private,omitempty"`
	DateCreated         *time.Time `json:"date_created,omitempty"`
	DateModified        *time.Time `json:"date_modified,omitempty"`
	Lines               []ProposalLine `json:"lines,omitempty"`
}

type ProposalLine struct {
	ID              int64   `json:"id"`
	Description     string  `json:"description"`
	ProductID       *int64  `json:"product_id,omitempty"`
	ProductRef      string  `json:"product_ref,omitempty"`
	ProductLabel    string  `json:"product_label,omitempty"`
	Qty             float64 `json:"qty"`
	UnitPrice       float64 `json:"unit_price"`
	VATRate         float64 `json:"vat_rate"`
	DiscountPercent float64 `json:"discount_percent,omitempty"`
	TotalHT         float64 `json:"total_ht"`
	TotalVAT        float64 `json:"total_vat"`
	TotalTTC        float64 `json:"total_ttc"`
	ProductType     int     `json:"product_type"`
	UnitID          *int64  `json:"unit_id,omitempty"`
	Rank            int     `json:"rank"`
	DateStart       *time.Time `json:"date_start,omitempty"`
	DateEnd         *time.Time `json:"date_end,omitempty"`
}

// Project
type Project struct {
	ID           int64      `json:"id"`
	Ref          string     `json:"ref"`
	Title        string     `json:"title"`
	Description  string     `json:"description,omitempty"`
	CustomerID   *int64     `json:"customer_id,omitempty"`
	CustomerName string     `json:"customer_name,omitempty"`
	Status       int        `json:"status_code"`
	StatusLabel  string     `json:"status"`
	Public       int        `json:"public"`
	DateStart    *time.Time `json:"date_start,omitempty"`
	DateEnd      *time.Time `json:"date_end,omitempty"`
	OppStatus    *int64     `json:"opp_status,omitempty"`
	OppAmount    float64    `json:"opp_amount,omitempty"`
	OppPercent   float64    `json:"opp_percent,omitempty"`
	Budget       float64    `json:"budget,omitempty"`
	NotePublic   string     `json:"note_public,omitempty"`
	NotePrivate  string     `json:"note_private,omitempty"`
	DateCreated  *time.Time `json:"date_created,omitempty"`
	Tasks        []ProjectTask `json:"tasks,omitempty"`
}

type ProjectTask struct {
	ID          int64      `json:"id"`
	Ref         string     `json:"ref"`
	Label       string     `json:"label"`
	Description string     `json:"description,omitempty"`
	DateStart   *time.Time `json:"date_start,omitempty"`
	DateEnd     *time.Time `json:"date_end,omitempty"`
	Progress    int        `json:"progress"`
	Planned     float64    `json:"planned_hours,omitempty"`
	Spent       float64    `json:"spent_hours,omitempty"`
	Status      int        `json:"status"`
}

// Order (commande)
type Order struct {
	ID               int64      `json:"id"`
	Ref              string     `json:"ref"`
	RefClient        string     `json:"ref_client,omitempty"`
	CustomerID       int64      `json:"customer_id"`
	CustomerName     string     `json:"customer_name,omitempty"`
	ProjectID        *int64     `json:"project_id,omitempty"`
	Status           int        `json:"status_code"`
	StatusLabel      string     `json:"status"`
	TotalHT          float64    `json:"total_ht"`
	TotalVAT         float64    `json:"total_vat"`
	TotalTTC         float64    `json:"total_ttc"`
	Currency         string     `json:"currency"`
	Date             *time.Time `json:"date,omitempty"`
	DeliveryDate     *time.Time `json:"delivery_date,omitempty"`
	PaymentTermLabel string     `json:"payment_term,omitempty"`
	PaymentModeLabel string     `json:"payment_mode,omitempty"`
	NotePublic       string     `json:"note_public,omitempty"`
	NotePrivate      string     `json:"note_private,omitempty"`
	Billed           int        `json:"billed"`
	Lines            []OrderLine `json:"lines,omitempty"`
}

type OrderLine struct {
	ID              int64   `json:"id"`
	Description     string  `json:"description"`
	ProductID       *int64  `json:"product_id,omitempty"`
	ProductRef      string  `json:"product_ref,omitempty"`
	ProductLabel    string  `json:"product_label,omitempty"`
	Qty             float64 `json:"qty"`
	UnitPrice       float64 `json:"unit_price"`
	VATRate         float64 `json:"vat_rate"`
	DiscountPercent float64 `json:"discount_percent,omitempty"`
	TotalHT         float64 `json:"total_ht"`
	TotalTTC        float64 `json:"total_ttc"`
	ProductType     int     `json:"product_type"`
	Rank            int     `json:"rank"`
}

// SupplierOrder (commande_fournisseur)
type SupplierOrder struct {
	ID               int64      `json:"id"`
	Ref              string     `json:"ref"`
	RefSupplier      string     `json:"ref_supplier,omitempty"`
	SupplierID       int64      `json:"supplier_id"`
	SupplierName     string     `json:"supplier_name,omitempty"`
	ProjectID        *int64     `json:"project_id,omitempty"`
	Status           int        `json:"status_code"`
	StatusLabel      string     `json:"status"`
	TotalHT          float64    `json:"total_ht"`
	TotalVAT         float64    `json:"total_vat"`
	TotalTTC         float64    `json:"total_ttc"`
	Currency         string     `json:"currency"`
	Date             *time.Time `json:"date,omitempty"`
	DeliveryDate     *time.Time `json:"delivery_date,omitempty"`
	PaymentTermLabel string     `json:"payment_term,omitempty"`
	NotePublic       string     `json:"note_public,omitempty"`
	NotePrivate      string     `json:"note_private,omitempty"`
	Billed           int        `json:"billed"`
	Lines            []OrderLine `json:"lines,omitempty"`
}

// Customer (societe)
type Customer struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	NameAlias    string     `json:"name_alias,omitempty"`
	Code         string     `json:"code,omitempty"`
	Status       int        `json:"status"`
	IsClient     int        `json:"is_client"`
	IsSupplier   int        `json:"is_supplier"`
	Address      string     `json:"address,omitempty"`
	Zip          string     `json:"zip,omitempty"`
	Town         string     `json:"town,omitempty"`
	Country      string     `json:"country,omitempty"`
	CountryCode  string     `json:"country_code,omitempty"`
	Phone        string     `json:"phone,omitempty"`
	Email        string     `json:"email,omitempty"`
	URL          string     `json:"url,omitempty"`
	TaxID        string     `json:"tax_id,omitempty"`
	NotePublic   string     `json:"note_public,omitempty"`
	NotePrivate  string     `json:"note_private,omitempty"`
	DateCreated  *time.Time `json:"date_created,omitempty"`
}

// Product
type Product struct {
	ID           int64      `json:"id"`
	Ref          string     `json:"ref"`
	Label        string     `json:"label"`
	Description  string     `json:"description,omitempty"`
	Type         int        `json:"type"`
	TypeLabel    string     `json:"type_label"`
	Status       int        `json:"status_sell"`
	StatusBuy    int        `json:"status_buy"`
	Price        float64    `json:"price"`
	PriceTTC     float64    `json:"price_ttc"`
	VATRate      float64    `json:"vat_rate"`
	CostPrice    float64    `json:"cost_price,omitempty"`
	Stock        float64    `json:"stock"`
	StockAlert   float64    `json:"stock_alert,omitempty"`
	Barcode      string     `json:"barcode,omitempty"`
	Weight       float64    `json:"weight,omitempty"`
	UnitID       *int64     `json:"unit_id,omitempty"`
	DateCreated  *time.Time `json:"date_created,omitempty"`
}

// Warehouse (entrepot)
type Warehouse struct {
	ID          int64      `json:"id"`
	Ref         string     `json:"ref"`
	Description string     `json:"description,omitempty"`
	Status      int        `json:"status"`
	Location    string     `json:"location,omitempty"`
	Address     string     `json:"address,omitempty"`
	Zip         string     `json:"zip,omitempty"`
	Town        string     `json:"town,omitempty"`
	ParentID    *int64     `json:"parent_id,omitempty"`
	ProjectID   *int64     `json:"project_id,omitempty"`
}

// Shipment (expedition)
type Shipment struct {
	ID              int64      `json:"id"`
	Ref             string     `json:"ref"`
	CustomerID      int64      `json:"customer_id"`
	CustomerName    string     `json:"customer_name,omitempty"`
	Status          int        `json:"status_code"`
	StatusLabel     string     `json:"status"`
	DateShipping    *time.Time `json:"date_shipping,omitempty"`
	DateDelivery    *time.Time `json:"date_delivery,omitempty"`
	TrackingNumber  string     `json:"tracking_number,omitempty"`
	ShippingMethod  string     `json:"shipping_method,omitempty"`
	Weight          float64    `json:"weight,omitempty"`
	OriginID        *int64     `json:"origin_id,omitempty"`
	OriginType      string     `json:"origin_type,omitempty"`
	NotePublic      string     `json:"note_public,omitempty"`
	NotePrivate     string     `json:"note_private,omitempty"`
	Lines           []ShipmentLine `json:"lines,omitempty"`
}

type ShipmentLine struct {
	ID          int64   `json:"id"`
	ProductID   *int64  `json:"product_id,omitempty"`
	ProductRef  string  `json:"product_ref,omitempty"`
	Description string  `json:"description,omitempty"`
	Qty         float64 `json:"qty"`
	WarehouseID *int64  `json:"warehouse_id,omitempty"`
}

// Reception
type Reception struct {
	ID              int64      `json:"id"`
	Ref             string     `json:"ref"`
	SupplierID      int64      `json:"supplier_id"`
	SupplierName    string     `json:"supplier_name,omitempty"`
	Status          int        `json:"status_code"`
	StatusLabel     string     `json:"status"`
	DateReception   *time.Time `json:"date_reception,omitempty"`
	DateDelivery    *time.Time `json:"date_delivery,omitempty"`
	OriginID        *int64     `json:"origin_id,omitempty"`
	OriginType      string     `json:"origin_type,omitempty"`
	NotePublic      string     `json:"note_public,omitempty"`
	NotePrivate     string     `json:"note_private,omitempty"`
	Lines           []ShipmentLine `json:"lines,omitempty"`
}

// Helper for scanning nullable times
func ScanNullTime(nt sql.NullTime) *time.Time {
	if nt.Valid {
		return &nt.Time
	}
	return nil
}

func ScanNullInt64(ni sql.NullInt64) *int64 {
	if ni.Valid {
		return &ni.Int64
	}
	return nil
}

func ScanNullString(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}
