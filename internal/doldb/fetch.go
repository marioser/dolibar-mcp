package doldb

import (
	"context"
	"database/sql"
	"fmt"

	"golang.org/x/sync/errgroup"
)

func (d *DB) Fetch(ctx context.Context, entity string, id int64, ref string) (any, error) {
	switch entity {
	case "proposals":
		return d.fetchProposal(ctx, id, ref)
	case "orders":
		return d.fetchOrder(ctx, id, ref)
	case "purchases":
		return d.fetchPurchase(ctx, id, ref)
	case "customers":
		return d.fetchCustomer(ctx, id, ref)
	case "products":
		return d.fetchProduct(ctx, id, ref)
	case "projects":
		return d.fetchProject(ctx, id, ref)
	case "warehouses":
		return d.fetchWarehouse(ctx, id, ref)
	case "shipments":
		return d.fetchShipment(ctx, id, ref)
	case "receptions":
		return d.fetchReception(ctx, id, ref)
	default:
		return nil, fmt.Errorf("unknown entity: %s", entity)
	}
}

func (d *DB) idOrRef(id int64, ref, alias string) (string, any) {
	if id > 0 {
		return fmt.Sprintf("%s.rowid = ?", alias), id
	}
	return fmt.Sprintf("%s.ref = ?", alias), ref
}

// --- PROPOSALS ---

func (d *DB) fetchProposal(ctx context.Context, id int64, ref string) (*Proposal, error) {
	where, arg := d.idOrRef(id, ref, "p")

	q := fmt.Sprintf(`SELECT p.rowid, p.ref, COALESCE(p.ref_client,''), p.entity,
		p.fk_soc, COALESCE(s.nom,''), COALESCE(s.code_client,''),
		p.fk_projet, p.fk_statut, COALESCE(st.label,''),
		p.datep, p.fin_validite, p.date_livraison,
		p.total_ht, p.total_tva, p.total_ttc,
		COALESCE(p.multicurrency_code,''), p.multicurrency_total_ttc,
		COALESCE(cr.libelle,''), COALESCE(cp.libelle,''), COALESCE(ca.label,''),
		COALESCE(p.note_public,''), COALESCE(p.note_private,''),
		p.datec, p.tms
	FROM %s p
	JOIN %s s ON s.rowid = p.fk_soc
	LEFT JOIN %s st ON st.id = p.fk_statut
	LEFT JOIN %s cr ON p.fk_cond_reglement = cr.rowid
	LEFT JOIN %s cp ON p.fk_mode_reglement = cp.id
	LEFT JOIN %s ca ON p.fk_availability = ca.rowid
	WHERE %s`,
		d.T("propal"), d.T("societe"), d.T("c_propalst"),
		d.T("c_payment_term"), d.T("c_paiement"), d.T("c_availability"),
		where)

	var p Proposal
	var projectID sql.NullInt64
	var datep, dfv, dliv, datec, tms sql.NullTime

	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return d.QueryRowContext(gCtx, q, arg).Scan(
			&p.ID, &p.Ref, &p.RefClient, &p.Entity,
			&p.CustomerID, &p.CustomerName, &p.CustomerCode,
			&projectID, &p.Status, &p.StatusLabel,
			&datep, &dfv, &dliv,
			&p.TotalHT, &p.TotalVAT, &p.TotalTTC,
			&p.MultiCurrencyCode, &p.MultiCurrencyTTC,
			&p.PaymentTermLabel, &p.PaymentModeLabel, &p.AvailabilityLabel,
			&p.NotePublic, &p.NotePrivate,
			&datec, &tms,
		)
	})

	var lines []ProposalLine
	g.Go(func() error {
		var targetID int64
		if id > 0 {
			targetID = id
		} else {
			// Need to resolve ref to id first for lines query
			row := d.QueryRowContext(gCtx, fmt.Sprintf("SELECT rowid FROM %s WHERE ref = ?", d.T("propal")), ref)
			if err := row.Scan(&targetID); err != nil {
				return err
			}
		}
		var err error
		lines, err = d.fetchProposalLines(gCtx, targetID)
		return err
	})

	if err := g.Wait(); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("proposal not found")
		}
		return nil, err
	}

	p.ProjectID = ScanNullInt64(projectID)
	p.Date = ScanNullTime(datep)
	p.DateEnd = ScanNullTime(dfv)
	p.DeliveryDate = ScanNullTime(dliv)
	p.DateCreated = ScanNullTime(datec)
	p.DateModified = ScanNullTime(tms)
	p.Currency = d.dolCfg.MainCurrency
	p.Lines = lines

	return &p, nil
}

func (d *DB) fetchProposalLines(ctx context.Context, proposalID int64) ([]ProposalLine, error) {
	q := fmt.Sprintf(`SELECT d.rowid, COALESCE(d.description,''), d.fk_product,
		COALESCE(pr.ref,''), COALESCE(pr.label,''),
		d.qty, d.subprice, d.tva_tx, d.remise_percent,
		d.total_ht, d.total_tva, d.total_ttc,
		d.product_type, d.fk_unit, d.rang, d.date_start, d.date_end
	FROM %s d
	LEFT JOIN %s pr ON d.fk_product = pr.rowid
	WHERE d.fk_propal = ?
	ORDER BY d.rang`, d.T("propaldet"), d.T("product"))

	rows, err := d.QueryContext(ctx, q, proposalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lines []ProposalLine
	for rows.Next() {
		var l ProposalLine
		var productID, unitID sql.NullInt64
		var dateStart, dateEnd sql.NullTime
		if err := rows.Scan(&l.ID, &l.Description, &productID,
			&l.ProductRef, &l.ProductLabel,
			&l.Qty, &l.UnitPrice, &l.VATRate, &l.DiscountPercent,
			&l.TotalHT, &l.TotalVAT, &l.TotalTTC,
			&l.ProductType, &unitID, &l.Rank, &dateStart, &dateEnd); err != nil {
			return nil, err
		}
		l.ProductID = ScanNullInt64(productID)
		l.UnitID = ScanNullInt64(unitID)
		l.DateStart = ScanNullTime(dateStart)
		l.DateEnd = ScanNullTime(dateEnd)
		lines = append(lines, l)
	}
	return lines, nil
}

// --- ORDERS ---

func (d *DB) fetchOrder(ctx context.Context, id int64, ref string) (*Order, error) {
	where, arg := d.idOrRef(id, ref, "c")
	q := fmt.Sprintf(`SELECT c.rowid, c.ref, COALESCE(c.ref_client,''),
		c.fk_soc, COALESCE(s.nom,''), c.fk_projet, c.fk_statut,
		c.total_ht, c.total_tva, c.total_ttc, COALESCE(c.multicurrency_code,''),
		c.date_commande, c.date_livraison,
		COALESCE(cr.libelle,''), COALESCE(cp.libelle,''),
		COALESCE(c.note_public,''), COALESCE(c.note_private,''), c.facture
	FROM %s c
	JOIN %s s ON s.rowid = c.fk_soc
	LEFT JOIN %s cr ON c.fk_cond_reglement = cr.rowid
	LEFT JOIN %s cp ON c.fk_mode_reglement = cp.id
	WHERE %s`, d.T("commande"), d.T("societe"), d.T("c_payment_term"), d.T("c_paiement"), where)

	var o Order
	var projectID sql.NullInt64
	var dateCmd, dateLiv sql.NullTime
	err := d.QueryRowContext(ctx, q, arg).Scan(
		&o.ID, &o.Ref, &o.RefClient,
		&o.CustomerID, &o.CustomerName, &projectID, &o.Status,
		&o.TotalHT, &o.TotalVAT, &o.TotalTTC, &o.Currency,
		&dateCmd, &dateLiv,
		&o.PaymentTermLabel, &o.PaymentModeLabel,
		&o.NotePublic, &o.NotePrivate, &o.Billed,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("order not found")
	}
	if err != nil {
		return nil, err
	}

	o.ProjectID = ScanNullInt64(projectID)
	o.Date = ScanNullTime(dateCmd)
	o.DeliveryDate = ScanNullTime(dateLiv)
	o.StatusLabel = statusLabel("orders", o.Status)
	if o.Currency == "" {
		o.Currency = d.dolCfg.MainCurrency
	}

	o.Lines, err = d.fetchOrderLines(ctx, o.ID)
	return &o, err
}

func (d *DB) fetchOrderLines(ctx context.Context, orderID int64) ([]OrderLine, error) {
	q := fmt.Sprintf(`SELECT d.rowid, COALESCE(d.description,''), d.fk_product,
		COALESCE(pr.ref,''), COALESCE(pr.label,''),
		d.qty, d.subprice, d.tva_tx, d.remise_percent,
		d.total_ht, d.total_ttc, d.product_type, d.rang
	FROM %s d
	LEFT JOIN %s pr ON d.fk_product = pr.rowid
	WHERE d.fk_commande = ?
	ORDER BY d.rang`, d.T("commandedet"), d.T("product"))

	rows, err := d.QueryContext(ctx, q, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lines []OrderLine
	for rows.Next() {
		var l OrderLine
		var productID sql.NullInt64
		if err := rows.Scan(&l.ID, &l.Description, &productID,
			&l.ProductRef, &l.ProductLabel,
			&l.Qty, &l.UnitPrice, &l.VATRate, &l.DiscountPercent,
			&l.TotalHT, &l.TotalTTC, &l.ProductType, &l.Rank); err != nil {
			return nil, err
		}
		l.ProductID = ScanNullInt64(productID)
		lines = append(lines, l)
	}
	return lines, nil
}

// --- PURCHASES ---

func (d *DB) fetchPurchase(ctx context.Context, id int64, ref string) (*SupplierOrder, error) {
	where, arg := d.idOrRef(id, ref, "c")
	q := fmt.Sprintf(`SELECT c.rowid, c.ref, COALESCE(c.ref_supplier,''),
		c.fk_soc, COALESCE(s.nom,''), c.fk_projet, c.fk_statut,
		c.total_ht, c.total_tva, c.total_ttc, COALESCE(c.multicurrency_code,''),
		c.date_commande, c.date_livraison,
		COALESCE(cr.libelle,''), COALESCE(c.note_public,''), COALESCE(c.note_private,''), c.billed
	FROM %s c
	JOIN %s s ON s.rowid = c.fk_soc
	LEFT JOIN %s cr ON c.fk_cond_reglement = cr.rowid
	WHERE %s`, d.T("commande_fournisseur"), d.T("societe"), d.T("c_payment_term"), where)

	var o SupplierOrder
	var projectID sql.NullInt64
	var dateCmd, dateLiv sql.NullTime
	err := d.QueryRowContext(ctx, q, arg).Scan(
		&o.ID, &o.Ref, &o.RefSupplier,
		&o.SupplierID, &o.SupplierName, &projectID, &o.Status,
		&o.TotalHT, &o.TotalVAT, &o.TotalTTC, &o.Currency,
		&dateCmd, &dateLiv,
		&o.PaymentTermLabel, &o.NotePublic, &o.NotePrivate, &o.Billed,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("supplier order not found")
	}
	if err != nil {
		return nil, err
	}

	o.ProjectID = ScanNullInt64(projectID)
	o.Date = ScanNullTime(dateCmd)
	o.DeliveryDate = ScanNullTime(dateLiv)
	o.StatusLabel = statusLabel("purchases", o.Status)
	if o.Currency == "" {
		o.Currency = d.dolCfg.MainCurrency
	}

	o.Lines, err = d.fetchPurchaseLines(ctx, o.ID)
	return &o, err
}

func (d *DB) fetchPurchaseLines(ctx context.Context, orderID int64) ([]OrderLine, error) {
	q := fmt.Sprintf(`SELECT d.rowid, COALESCE(d.description,''), d.fk_product,
		COALESCE(pr.ref,''), COALESCE(pr.label,''),
		d.qty, d.subprice, d.tva_tx, d.remise_percent,
		d.total_ht, d.total_ttc, d.product_type, d.rang
	FROM %s d
	LEFT JOIN %s pr ON d.fk_product = pr.rowid
	WHERE d.fk_commande = ?
	ORDER BY d.rang`, d.T("commande_fournisseurdet"), d.T("product"))

	rows, err := d.QueryContext(ctx, q, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lines []OrderLine
	for rows.Next() {
		var l OrderLine
		var productID sql.NullInt64
		if err := rows.Scan(&l.ID, &l.Description, &productID,
			&l.ProductRef, &l.ProductLabel,
			&l.Qty, &l.UnitPrice, &l.VATRate, &l.DiscountPercent,
			&l.TotalHT, &l.TotalTTC, &l.ProductType, &l.Rank); err != nil {
			return nil, err
		}
		l.ProductID = ScanNullInt64(productID)
		lines = append(lines, l)
	}
	return lines, nil
}

// --- CUSTOMERS ---

func (d *DB) fetchCustomer(ctx context.Context, id int64, ref string) (*Customer, error) {
	where, arg := d.idOrRef(id, ref, "s")
	q := fmt.Sprintf(`SELECT s.rowid, s.nom, COALESCE(s.name_alias,''), COALESCE(s.code_client,''),
		s.status, s.client, s.fournisseur,
		COALESCE(s.address,''), COALESCE(s.zip,''), COALESCE(s.town,''),
		COALESCE(c.label,''), COALESCE(c.code,''),
		COALESCE(s.phone,''), COALESCE(s.email,''), COALESCE(s.url,''),
		COALESCE(s.tva_intra,''),
		COALESCE(s.note_public,''), COALESCE(s.note_private,''), s.datec
	FROM %s s
	LEFT JOIN %s c ON s.fk_pays = c.rowid
	WHERE %s`, d.T("societe"), d.T("c_country"), where)

	var cu Customer
	var datec sql.NullTime
	err := d.QueryRowContext(ctx, q, arg).Scan(
		&cu.ID, &cu.Name, &cu.NameAlias, &cu.Code,
		&cu.Status, &cu.IsClient, &cu.IsSupplier,
		&cu.Address, &cu.Zip, &cu.Town,
		&cu.Country, &cu.CountryCode,
		&cu.Phone, &cu.Email, &cu.URL, &cu.TaxID,
		&cu.NotePublic, &cu.NotePrivate, &datec,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("customer not found")
	}
	if err != nil {
		return nil, err
	}
	cu.DateCreated = ScanNullTime(datec)
	return &cu, nil
}

// --- PRODUCTS ---

func (d *DB) fetchProduct(ctx context.Context, id int64, ref string) (*Product, error) {
	where, arg := d.idOrRef(id, ref, "p")
	q := fmt.Sprintf(`SELECT p.rowid, p.ref, p.label, COALESCE(p.description,''),
		p.fk_product_type, p.tosell, p.tobuy,
		p.price, p.price_ttc, p.tva_tx, COALESCE(p.cost_price,0),
		COALESCE(p.stock,0), COALESCE(p.seuil_stock_alerte,0),
		COALESCE(p.barcode,''), COALESCE(p.weight,0), p.fk_unit, p.datec
	FROM %s p
	WHERE %s`, d.T("product"), where)

	var pr Product
	var unitID sql.NullInt64
	var datec sql.NullTime
	err := d.QueryRowContext(ctx, q, arg).Scan(
		&pr.ID, &pr.Ref, &pr.Label, &pr.Description,
		&pr.Type, &pr.Status, &pr.StatusBuy,
		&pr.Price, &pr.PriceTTC, &pr.VATRate, &pr.CostPrice,
		&pr.Stock, &pr.StockAlert,
		&pr.Barcode, &pr.Weight, &unitID, &datec,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("product not found")
	}
	if err != nil {
		return nil, err
	}
	pr.UnitID = ScanNullInt64(unitID)
	pr.DateCreated = ScanNullTime(datec)
	if pr.Type == 1 {
		pr.TypeLabel = "Service"
	} else {
		pr.TypeLabel = "Product"
	}
	return &pr, nil
}

// --- PROJECTS ---

func (d *DB) fetchProject(ctx context.Context, id int64, ref string) (*Project, error) {
	where, arg := d.idOrRef(id, ref, "pj")
	q := fmt.Sprintf(`SELECT pj.rowid, pj.ref, pj.title, COALESCE(pj.description,''),
		pj.fk_soc, COALESCE(s.nom,''), pj.fk_statut, pj.public,
		pj.dateo, pj.datee, pj.fk_opp_status,
		COALESCE(pj.opp_amount,0), COALESCE(pj.opp_percent,0), COALESCE(pj.budget_amount,0),
		COALESCE(pj.note_public,''), COALESCE(pj.note_private,''), pj.datec
	FROM %s pj
	LEFT JOIN %s s ON s.rowid = pj.fk_soc
	WHERE %s`, d.T("projet"), d.T("societe"), where)

	var pj Project
	var custID, oppStatus sql.NullInt64
	var dateStart, dateEnd, datec sql.NullTime
	err := d.QueryRowContext(ctx, q, arg).Scan(
		&pj.ID, &pj.Ref, &pj.Title, &pj.Description,
		&custID, &pj.CustomerName, &pj.Status, &pj.Public,
		&dateStart, &dateEnd, &oppStatus,
		&pj.OppAmount, &pj.OppPercent, &pj.Budget,
		&pj.NotePublic, &pj.NotePrivate, &datec,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("project not found")
	}
	if err != nil {
		return nil, err
	}

	pj.CustomerID = ScanNullInt64(custID)
	pj.OppStatus = ScanNullInt64(oppStatus)
	pj.DateStart = ScanNullTime(dateStart)
	pj.DateEnd = ScanNullTime(dateEnd)
	pj.DateCreated = ScanNullTime(datec)
	pj.StatusLabel = statusLabel("projects", pj.Status)

	pj.Tasks, _ = d.fetchProjectTasks(ctx, pj.ID)
	return &pj, nil
}

func (d *DB) fetchProjectTasks(ctx context.Context, projectID int64) ([]ProjectTask, error) {
	q := fmt.Sprintf(`SELECT t.rowid, t.ref, t.label, COALESCE(t.description,''),
		t.dateo, t.datee, COALESCE(t.progress,0), COALESCE(t.planned_workload,0),
		COALESCE(t.duration_effective,0), COALESCE(t.fk_statut,0)
	FROM %s t WHERE t.fk_projet = ? ORDER BY t.rang, t.dateo`, d.T("projet_task"))

	rows, err := d.QueryContext(ctx, q, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []ProjectTask
	for rows.Next() {
		var t ProjectTask
		var dateStart, dateEnd sql.NullTime
		if err := rows.Scan(&t.ID, &t.Ref, &t.Label, &t.Description,
			&dateStart, &dateEnd, &t.Progress, &t.Planned, &t.Spent, &t.Status); err != nil {
			return nil, err
		}
		t.DateStart = ScanNullTime(dateStart)
		t.DateEnd = ScanNullTime(dateEnd)
		tasks = append(tasks, t)
	}
	return tasks, nil
}

// --- WAREHOUSES ---

func (d *DB) fetchWarehouse(ctx context.Context, id int64, ref string) (*Warehouse, error) {
	where, arg := d.idOrRef(id, ref, "w")
	q := fmt.Sprintf(`SELECT w.rowid, w.ref, COALESCE(w.description,''), w.statut,
		COALESCE(w.lieu,''), COALESCE(w.address,''), COALESCE(w.zip,''), COALESCE(w.town,''),
		w.fk_parent, w.fk_project
	FROM %s w WHERE %s`, d.T("entrepot"), where)

	var w Warehouse
	var parentID, projectID sql.NullInt64
	err := d.QueryRowContext(ctx, q, arg).Scan(
		&w.ID, &w.Ref, &w.Description, &w.Status,
		&w.Location, &w.Address, &w.Zip, &w.Town,
		&parentID, &projectID,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("warehouse not found")
	}
	if err != nil {
		return nil, err
	}
	w.ParentID = ScanNullInt64(parentID)
	w.ProjectID = ScanNullInt64(projectID)
	return &w, nil
}

// --- SHIPMENTS ---

func (d *DB) fetchShipment(ctx context.Context, id int64, ref string) (*Shipment, error) {
	where, arg := d.idOrRef(id, ref, "e")
	q := fmt.Sprintf(`SELECT e.rowid, e.ref, e.fk_soc, COALESCE(s.nom,''),
		e.fk_statut, e.date_expedition, e.date_delivery,
		COALESCE(e.tracking_number,''), COALESCE(sm.libelle,''),
		COALESCE(e.weight,0),
		el.fk_source, COALESCE(el.sourcetype,''),
		COALESCE(e.note_public,''), COALESCE(e.note_private,'')
	FROM %s e
	JOIN %s s ON s.rowid = e.fk_soc
	LEFT JOIN %s sm ON e.fk_shipping_method = sm.rowid
	LEFT JOIN %s el ON el.fk_target = e.rowid AND el.targettype = 'shipping'
	WHERE %s`, d.T("expedition"), d.T("societe"), d.T("c_shipment_mode"), d.T("element_element"), where)

	var sh Shipment
	var dateShip, dateDel sql.NullTime
	var originID sql.NullInt64
	err := d.QueryRowContext(ctx, q, arg).Scan(
		&sh.ID, &sh.Ref, &sh.CustomerID, &sh.CustomerName,
		&sh.Status, &dateShip, &dateDel,
		&sh.TrackingNumber, &sh.ShippingMethod, &sh.Weight,
		&originID, &sh.OriginType,
		&sh.NotePublic, &sh.NotePrivate,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("shipment not found")
	}
	if err != nil {
		return nil, err
	}
	sh.DateShipping = ScanNullTime(dateShip)
	sh.DateDelivery = ScanNullTime(dateDel)
	sh.OriginID = ScanNullInt64(originID)
	sh.StatusLabel = statusLabel("shipments", sh.Status)
	return &sh, nil
}

// --- RECEPTIONS ---

func (d *DB) fetchReception(ctx context.Context, id int64, ref string) (*Reception, error) {
	where, arg := d.idOrRef(id, ref, "r")
	q := fmt.Sprintf(`SELECT r.rowid, r.ref, r.fk_soc, COALESCE(s.nom,''),
		r.fk_statut, r.date_reception, r.date_delivery,
		el.fk_source, COALESCE(el.sourcetype,''),
		COALESCE(r.note_public,''), COALESCE(r.note_private,'')
	FROM %s r
	JOIN %s s ON s.rowid = r.fk_soc
	LEFT JOIN %s el ON el.fk_target = r.rowid AND el.targettype = 'reception'
	WHERE %s`, d.T("reception"), d.T("societe"), d.T("element_element"), where)

	var rc Reception
	var dateRec, dateDel sql.NullTime
	var originID sql.NullInt64
	err := d.QueryRowContext(ctx, q, arg).Scan(
		&rc.ID, &rc.Ref, &rc.SupplierID, &rc.SupplierName,
		&rc.Status, &dateRec, &dateDel,
		&originID, &rc.OriginType,
		&rc.NotePublic, &rc.NotePrivate,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("reception not found")
	}
	if err != nil {
		return nil, err
	}
	rc.DateReception = ScanNullTime(dateRec)
	rc.DateDelivery = ScanNullTime(dateDel)
	rc.OriginID = ScanNullInt64(originID)
	rc.StatusLabel = statusLabel("receptions", rc.Status)
	return &rc, nil
}
