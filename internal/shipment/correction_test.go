package shipment

import "testing"

func TestValidateCorrectionLimitsShipmentEditsToSafeFields(t *testing.T) {
	req := CorrectionRequest{
		ExpectedVersion:     1,
		Reason:              "Corrected recipient phone",
		ReferenceNumber:     "ORDER-100",
		ContentDescription:  "Documents",
		SpecialInstructions: "Call before delivery",
		Sender: CorrectionAddress{
			ContactName: "Sender Name", Phone: "+2348011111111", Line1: "12 Origin Street",
		},
		Recipient: CorrectionAddress{
			ContactName: "Recipient Name", Phone: "+2348022222222", Line1: "34 Destination Road",
		},
	}
	if err := ValidateCorrection(&req); err != nil {
		t.Fatalf("valid correction rejected: %v", err)
	}

	req.ExpectedVersion = 0
	req.Reason = "bad"
	req.Recipient.Phone = "1"
	if err := ValidateCorrection(&req); err == nil {
		t.Fatal("invalid correction accepted")
	}
}
