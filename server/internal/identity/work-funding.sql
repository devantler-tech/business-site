-- #268 (ADR 0030, decision 6): a work approval binds the order's settled
-- funding charge instead of a retired invoice record. Earlier rows keep their
-- invoice binding unchanged; every new row names its order and charge, whose
-- request, client, acceptance and exact amounts the database enforces.
ALTER TABLE portal_orders ADD CONSTRAINT portal_order_work_binding UNIQUE(id,request_id,client_id,acceptance_id);
ALTER TABLE portal_order_charges ADD CONSTRAINT portal_charge_work_binding UNIQUE(order_id,period,gross_minor,net_minor,vat_minor);
ALTER TABLE portal_work_approvals
  ALTER COLUMN invoice_sequence DROP NOT NULL,
  ADD COLUMN order_id text,
  ADD COLUMN charge_period text,
  ADD CONSTRAINT portal_work_approval_one_binding CHECK(
    (invoice_sequence IS NOT NULL AND order_id IS NULL AND charge_period IS NULL) OR
    (invoice_sequence IS NULL AND order_id IS NOT NULL AND charge_period IS NOT NULL)),
  ADD CONSTRAINT portal_work_approval_order FOREIGN KEY(order_id,request_id,client_id,acceptance_id)
    REFERENCES portal_orders(id,request_id,client_id,acceptance_id),
  ADD CONSTRAINT portal_work_approval_charge FOREIGN KEY(order_id,charge_period,amount_minor,net_minor,vat_minor)
    REFERENCES portal_order_charges(order_id,period,gross_minor,net_minor,vat_minor);
