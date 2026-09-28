package pricing

import (
	"testing"

	"github.com/ceserve/courier-os/internal/platform/money"
)

func TestComputeInquiryDiscountPercentageUsesRemainingEligiblePrice(t *testing.T) {
	percentage := int32(1_000)
	in := QuoteInput{
		Currency: money.NGN,
		InquiryDiscount: &InquiryDiscountInput{
			DiscountType: "PERCENTAGE",
			PercentageBP: &percentage,
		},
	}

	discount, item := computeInquiryDiscount(in, 10_000, 1_000)
	if discount == nil {
		t.Fatal("expected an inquiry discount")
	}
	if discount.BasisMinor != 9_000 || discount.AmountMinor != 900 {
		t.Fatalf("basis/amount = %d/%d, want 9000/900", discount.BasisMinor, discount.AmountMinor)
	}
	if item.Code != "INQUIRY_DISCOUNT" || item.AmountMinor != -900 {
		t.Fatalf("line item = %#v, want INQUIRY_DISCOUNT at -900", item)
	}
}

func TestComputeInquiryDiscountFixedIsCappedAtRemainingEligiblePrice(t *testing.T) {
	value := int64(5_000)
	in := QuoteInput{
		Currency: money.NGN,
		InquiryDiscount: &InquiryDiscountInput{
			DiscountType: "FIXED",
			ValueMinor:   &value,
		},
	}

	discount, item := computeInquiryDiscount(in, 4_000, 1_000)
	if discount == nil {
		t.Fatal("expected an inquiry discount")
	}
	if discount.BasisMinor != 3_000 || discount.AmountMinor != 3_000 {
		t.Fatalf("basis/amount = %d/%d, want 3000/3000", discount.BasisMinor, discount.AmountMinor)
	}
	if item.AmountMinor != -3_000 {
		t.Fatalf("line amount = %d, want -3000", item.AmountMinor)
	}
}

func TestComputeInquiryDiscountAbsent(t *testing.T) {
	discount, item := computeInquiryDiscount(QuoteInput{Currency: money.NGN}, 10_000, 0)
	if discount != nil {
		t.Fatalf("discount = %#v, want nil", discount)
	}
	if item != (LineItem{}) {
		t.Fatalf("line item = %#v, want zero value", item)
	}
}
