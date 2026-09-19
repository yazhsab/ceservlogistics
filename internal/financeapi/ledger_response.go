package financeapi

import (
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
)

// Transport projections expose the documented public identifiers and values,
// never database rows or tenant/audit implementation fields.

type journalView struct {
	ID                string     `json:"id"`
	TransactionNumber string     `json:"transactionNumber"`
	Status            string     `json:"status"`
	PostingDate       string     `json:"postingDate"`
	Currency          string     `json:"currency"`
	TotalMinor        int64      `json:"totalMinor"`
	EntryCount        int32      `json:"entryCount"`
	SourceType        string     `json:"sourceType"`
	SourceID          *string    `json:"sourceId"`
	Purpose           string     `json:"purpose"`
	Description       string     `json:"description"`
	Reason            *string    `json:"reason"`
	ReversesID        *int64     `json:"reversesId"`
	ReversedByID      *int64     `json:"reversedById"`
	PostedAt          *time.Time `json:"postedAt"`
}

func journalDetailResponse(row dbgen.GetJournalByPublicIDRow) journalView {
	return journalView{
		ID:                row.PublicID,
		TransactionNumber: row.TransactionNumber,
		Status:            row.Status,
		PostingDate:       row.PostingDate.Format("2006-01-02"),
		Currency:          row.Currency,
		TotalMinor:        row.TotalMinor,
		EntryCount:        row.EntryCount,
		SourceType:        row.SourceType,
		SourceID:          row.SourcePublicID,
		Purpose:           row.Purpose,
		Description:       row.Description,
		Reason:            row.Reason,
		ReversesID:        row.ReversesID,
		ReversedByID:      row.ReversedByID,
		PostedAt:          row.PostedAt,
	}
}

func journalListResponse(row dbgen.ListJournalTransactionsRow) journalView {
	return journalView{
		ID:                row.PublicID,
		TransactionNumber: row.TransactionNumber,
		Status:            row.Status,
		PostingDate:       row.PostingDate.Format("2006-01-02"),
		Currency:          row.Currency,
		TotalMinor:        row.TotalMinor,
		EntryCount:        row.EntryCount,
		SourceType:        row.SourceType,
		SourceID:          row.SourcePublicID,
		Purpose:           row.Purpose,
		Description:       row.Description,
		Reason:            row.Reason,
		ReversesID:        row.ReversesID,
		ReversedByID:      row.ReversedByID,
		PostedAt:          row.PostedAt,
	}
}

type journalEntryView struct {
	ID          string  `json:"id"`
	LineNo      int32   `json:"lineNo"`
	AccountCode string  `json:"accountCode"`
	AccountName string  `json:"accountName"`
	DebitMinor  int64   `json:"debitMinor"`
	CreditMinor int64   `json:"creditMinor"`
	Currency    string  `json:"currency"`
	Memo        *string `json:"memo"`
}

func journalEntryResponse(row dbgen.ListJournalEntriesRow) journalEntryView {
	return journalEntryView{
		ID:          row.PublicID,
		LineNo:      row.LineNo,
		AccountCode: row.AccountCode,
		AccountName: row.AccountName,
		DebitMinor:  row.DebitMinor,
		CreditMinor: row.CreditMinor,
		Currency:    row.Currency,
		Memo:        row.Memo,
	}
}

type periodView struct {
	ID          string     `json:"id"`
	Code        string     `json:"code"`
	StartsOn    string     `json:"startsOn"`
	EndsOn      string     `json:"endsOn"`
	Status      string     `json:"status"`
	ClosedAt    *time.Time `json:"closedAt"`
	CloseReason *string    `json:"closeReason"`
}

func periodResponse(row dbgen.AccountingPeriod) periodView {
	return periodView{
		ID:          row.PublicID,
		Code:        row.Code,
		StartsOn:    row.StartsOn.Format("2006-01-02"),
		EndsOn:      row.EndsOn.Format("2006-01-02"),
		Status:      row.Status,
		ClosedAt:    row.ClosedAt,
		CloseReason: row.CloseReason,
	}
}
