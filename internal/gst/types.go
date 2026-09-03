package gst

// GSTR-1 JSON schema types matching the GSTN "GSTR-1 JSON v3.2.x" structure
// used to upload returns in the GST Portal / Offline Utility. Field names and
// shapes follow the validated portal schema:
//
//   - invoices are grouped under "b2b" by recipient GSTIN ("ctin")
//   - item detail ("itm_det") carries only financials: rt, txval, iamt, camt,
//     samt, csamt (no HSN — the HSN is reported in the separate Table 12)
//   - dates are DD-MM-YYYY, HSN/UQC live in the "hsn" summary and document
//     series in "doc_issue" (Table 13)
type GSTR1 struct {
	Gstin    string      `json:"gstin"`
	Fp       string      `json:"fp"`
	Gt       float64     `json:"gt"`
	CurtGt   float64     `json:"cur_gt"`
	Version  string      `json:"version"`
	B2B      []B2BEntry  `json:"b2b"`
	B2CL     []B2CLEntry `json:"b2cl"`
	B2CS     []B2CSItem  `json:"b2cs"`
	Cdnr     []CDNREntry `json:"cdnr"`
	Cdnur    []CDNURNote `json:"cdnur"`
	Hsn      HSNSection  `json:"hsn"`
	DocIssue DocIssue    `json:"doc_issue"`
}

// ItmDet is the per-rate item detail of an invoice or credit/debit note.
// Tax amounts are stored/emitted as numbers; zero components are omitted so
// the file carries only the components relevant to the supply type.
type ItmDet struct {
	Rt    float64 `json:"rt"`
	Txval float64 `json:"txval"`
	Iamt  float64 `json:"iamt,omitempty"`
	Camt  float64 `json:"camt,omitempty"`
	Samt  float64 `json:"samt,omitempty"`
	Csamt float64 `json:"csamt,omitempty"`
}

// B2BLineItem is one line of an invoice/note ("itms").
type B2BLineItem struct {
	Num    int    `json:"num"`
	ItmDet ItmDet `json:"itm_det"`
}

// B2BEntry groups all invoices issued to one registered recipient.
type B2BEntry struct {
	Ctin string       `json:"ctin"`
	Inv  []B2BInvoice `json:"inv"`
}

// B2BInvoice is a single invoice in the B2B/B2CL tables.
type B2BInvoice struct {
	Inum   string        `json:"inum"`
	Idt    string        `json:"idt"`
	Val    float64       `json:"val"`
	Pos    string        `json:"pos"`
	Rchrg  string        `json:"rchrg"`
	Etin   string        `json:"etin,omitempty"`
	InvTyp string        `json:"inv_typ"`
	Itms   []B2BLineItem `json:"itms"`
}

// B2CLEntry groups unregistered inter-state invoices above the B2CL
// threshold by place of supply.
type B2CLEntry struct {
	Pos string       `json:"pos"`
	Inv []B2BInvoice `json:"inv"`
}

// B2CSItem is one consolidated B2CS row (Table 7): aggregated per
// place of supply, rate and supply type.
type B2CSItem struct {
	Pos    string  `json:"pos"`
	Typ    string  `json:"typ,omitempty"`
	SplyTy string  `json:"sply_ty"`
	Rt     float64 `json:"rt"`
	Txval  float64 `json:"txval"`
	Iamt   float64 `json:"iamt,omitempty"`
	Camt   float64 `json:"camt,omitempty"`
	Samt   float64 `json:"samt,omitempty"`
	Csamt  float64 `json:"csamt,omitempty"`
}

// CDNREntry groups credit/debit notes issued to one registered recipient.
type CDNREntry struct {
	Ctin string          `json:"ctin"`
	Nt   []CDNREntryNote `json:"nt"`
}

// CDNREntryNote is a single credit/debit note reported to a registered
// recipient (Table 9A).
type CDNREntryNote struct {
	NtNum  string        `json:"nt_num"`
	NtDt   string        `json:"nt_dt"`
	Ntty   string        `json:"ntty"`
	Val    float64       `json:"val"`
	Pos    string        `json:"pos"`
	Rchrg  string        `json:"rchrg"`
	Etin   string        `json:"etin,omitempty"`
	Inum   string        `json:"inum,omitempty"`
	Idt    string        `json:"idt,omitempty"`
	Rsn    string        `json:"rsn,omitempty"`
	Pgst   string        `json:"p_gst,omitempty"`
	InvTyp string        `json:"inv_typ"`
	Itms   []B2BLineItem `json:"itms"`
}

// CDNURNote is a credit/debit note issued to an unregistered recipient
// (Table 9B).
type CDNURNote struct {
	Typ    string        `json:"typ"`
	NtNum  string        `json:"nt_num"`
	NtDt   string        `json:"nt_dt"`
	Ntty   string        `json:"ntty"`
	Val    float64       `json:"val"`
	Pos    string        `json:"pos"`
	Rchrg  string        `json:"rchrg"`
	Inum   string        `json:"inum,omitempty"`
	Idt    string        `json:"idt,omitempty"`
	Rsn    string        `json:"rsn,omitempty"`
	Pgst   string        `json:"p_gst,omitempty"`
	InvTyp string        `json:"inv_typ"`
	Itms   []B2BLineItem `json:"itms"`
}

// HSNSection is the Table 12 HSN summary. The "data" wrapper matches the
// shape written by the GST Offline Utility.
type HSNSection struct {
	Data []HSNSummary `json:"data"`
}

// HSNSummary is one aggregated HSN row.
type HSNSummary struct {
	Num     int     `json:"num"`
	HSNCode string  `json:"hsn_sc"`
	Desc    string  `json:"desc"`
	UQC     string  `json:"uqc"`
	Qty     float64 `json:"qty"`
	Txval   float64 `json:"txval"`
	Rt      float64 `json:"rt"`
	Iamt    float64 `json:"iamt,omitempty"`
	Camt    float64 `json:"camt,omitempty"`
	Samt    float64 `json:"samt,omitempty"`
	Csamt   float64 `json:"csamt,omitempty"`
}

// DocIssue is the Table 13 summary of documents issued, per document type
// and per numeric series.
type DocIssue struct {
	DocDet []DocDetail `json:"doc_det"`
}

// DocDetail is one document type ("Invoices for outward supply",
// "Credit Note", ...) with its series ranges.
type DocDetail struct {
	DocNum int        `json:"doc_num"`
	DocTyp string     `json:"doc_typ"`
	Docs   []DocRange `json:"docs"`
}

// DocRange is one continuous numeric series of a document type.
type DocRange struct {
	Num      int     `json:"num"`
	From     string  `json:"from"`
	To       string  `json:"to"`
	TotNum   float64 `json:"totnum"`
	Cancel   float64 `json:"cancel"`
	NetIssue float64 `json:"net_issue"`
}
