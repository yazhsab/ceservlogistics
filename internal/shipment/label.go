package shipment

import (
	"fmt"
	"strings"
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/money"
)

// Label is the barcode/QR-ready label payload.
//
// Constitution §M08: a label payload is produced synchronously; PDF rendering
// is a Release 4 background job. Everything a label printer or a client-side
// renderer needs is here, including the exact barcode and QR payload strings,
// so no consumer has to reconstruct them from other fields and risk drift.
type Label struct {
	AWB             string `json:"awb"`
	ShipmentID      string `json:"shipmentId"`
	ReferenceNumber string `json:"referenceNumber,omitempty"`

	// BarcodePayload is the Code128 content: the AWB, nothing else, so a scan
	// anywhere in the network yields a value the API accepts directly.
	BarcodePayload string `json:"barcodePayload"`
	BarcodeFormat  string `json:"barcodeFormat"`
	// QRPayload carries a compact pipe-delimited record for handheld scanners
	// that need destination context without a network round trip.
	QRPayload string `json:"qrPayload"`

	CarrierCode string `json:"carrierCode"`
	ServiceCode string `json:"serviceCode"`
	ServiceName string `json:"serviceName"`
	ServiceMode string `json:"serviceMode"`

	// RoutingCode is the sortation string printed large on the label.
	RoutingCode string `json:"routingCode"`

	OriginBranchCode      string `json:"originBranchCode,omitempty"`
	OriginHubCode         string `json:"originHubCode,omitempty"`
	DestinationHubCode    string `json:"destinationHubCode,omitempty"`
	DestinationBranchCode string `json:"destinationBranchCode,omitempty"`

	Sender    LabelParty `json:"sender"`
	Recipient LabelParty `json:"recipient"`

	PieceCount            int32  `json:"pieceCount"`
	ActualWeightGrams     int32  `json:"actualWeightGrams"`
	ChargeableWeightGrams int32  `json:"chargeableWeightGrams"`
	WeightLabel           string `json:"weightLabel"`

	PaymentMode        string `json:"paymentMode"`
	CODAmountMinor     int64  `json:"codAmountMinor"`
	CODAmountLabel     string `json:"codAmountLabel,omitempty"`
	DeclaredValueMinor int64  `json:"declaredValueMinor"`
	Currency           string `json:"currency"`

	ContentDescription  string `json:"contentDescription"`
	SpecialInstructions string `json:"specialInstructions,omitempty"`
	IsFragile           bool   `json:"isFragile"`

	BookedAt           time.Time  `json:"bookedAt"`
	PromisedDeliveryAt *time.Time `json:"promisedDeliveryAt,omitempty"`
	GeneratedAt        time.Time  `json:"generatedAt"`

	Pieces []LabelPiece `json:"pieces"`
}

// LabelParty is one addressed party on the label.
type LabelParty struct {
	Name     string `json:"name"`
	Company  string `json:"company,omitempty"`
	Phone    string `json:"phone"`
	Line1    string `json:"line1"`
	Line2    string `json:"line2,omitempty"`
	Landmark string `json:"landmark,omitempty"`
	City     string `json:"city"`
	State    string `json:"state"`
	Pincode  string `json:"pincode"`
}

// LabelPiece is one scannable piece.
type LabelPiece struct {
	Sequence          int32  `json:"sequence"`
	Barcode           string `json:"barcode"`
	ActualWeightGrams int32  `json:"actualWeightGrams"`
	Dimensions        string `json:"dimensions,omitempty"`
}

func buildLabel(d dbgen.GetShipmentLabelDataRow, packages []dbgen.ShipmentPackage, carrierCode string) Label {
	currency := money.Currency(d.Currency)
	l := Label{
		AWB: d.Awb, ShipmentID: d.PublicID,
		BarcodePayload: d.Awb, BarcodeFormat: "CODE128",
		CarrierCode: carrierCode,
		ServiceCode: d.ServiceCode, ServiceName: d.ServiceName, ServiceMode: d.ServiceMode,
		OriginBranchCode: deref(d.OriginBranchCode), OriginHubCode: deref(d.OriginHubCode),
		DestinationHubCode: deref(d.DestinationHubCode), DestinationBranchCode: deref(d.DestinationBranchCode),
		PieceCount: d.PieceCount, ActualWeightGrams: d.ActualWeightGrams,
		ChargeableWeightGrams: d.ChargeableWeightGrams,
		WeightLabel:           formatWeight(d.ChargeableWeightGrams),
		PaymentMode:           d.PaymentMode, CODAmountMinor: d.CodAmountMinor,
		DeclaredValueMinor: d.DeclaredValueMinor, Currency: d.Currency,
		ContentDescription: d.ContentDescription, IsFragile: d.IsFragile,
		BookedAt: d.BookedAt, PromisedDeliveryAt: d.PromisedDeliveryAt,
		GeneratedAt: time.Now(),
		Pieces:      []LabelPiece{},
	}
	if d.ReferenceNumber != nil {
		l.ReferenceNumber = *d.ReferenceNumber
	}
	if d.SpecialInstructions != nil {
		l.SpecialInstructions = *d.SpecialInstructions
	}
	if d.CodAmountMinor > 0 {
		l.CODAmountLabel = string(currency) + " " + money.FormatMinor(d.CodAmountMinor, currency.Exponent())
	}

	// The sortation code is what a hub operator reads to throw the parcel into
	// the right bag: destination hub, destination branch, destination PIN.
	l.RoutingCode = strings.Join(nonEmpty(
		deref(d.DestinationHubCode), deref(d.DestinationBranchCode), deref(d.RecipientPincode),
	), "/")

	l.Sender = LabelParty{
		Name: deref(d.SenderName), Company: deref(d.SenderCompany), Phone: deref(d.SenderPhone),
		Line1: deref(d.SenderLine1), Line2: deref(d.SenderLine2),
		City: deref(d.SenderCity), State: deref(d.SenderState), Pincode: deref(d.SenderPincode),
	}
	l.Recipient = LabelParty{
		Name: deref(d.RecipientName), Company: deref(d.RecipientCompany), Phone: deref(d.RecipientPhone),
		Line1: deref(d.RecipientLine1), Line2: deref(d.RecipientLine2), Landmark: deref(d.RecipientLandmark),
		City: deref(d.RecipientCity), State: deref(d.RecipientState), Pincode: deref(d.RecipientPincode),
	}

	// Compact, fixed-order QR payload. Version-prefixed so a scanner can detect
	// a format change rather than misparse it.
	l.QRPayload = strings.Join([]string{
		"CSV1", d.Awb, d.ServiceCode,
		deref(d.DestinationBranchCode), deref(d.RecipientPincode),
		fmt.Sprint(d.PieceCount), fmt.Sprint(d.ChargeableWeightGrams),
		d.PaymentMode, fmt.Sprint(d.CodAmountMinor),
	}, "|")

	for _, p := range packages {
		piece := LabelPiece{
			Sequence: p.Sequence, Barcode: p.PieceBarcode, ActualWeightGrams: p.ActualWeightGrams,
		}
		if p.LengthMm != nil && p.WidthMm != nil && p.HeightMm != nil {
			piece.Dimensions = fmt.Sprintf("%dx%dx%d mm", *p.LengthMm, *p.WidthMm, *p.HeightMm)
		}
		l.Pieces = append(l.Pieces, piece)
	}
	return l
}

// renderZPL produces a 4x6 inch thermal label at 203 dpi.
//
// ZPL is emitted directly rather than through a PDF pipeline because branch
// counters print to Zebra-compatible hardware and a text payload is far cheaper
// to generate and transmit than a rendered document.
func renderZPL(l Label) string {
	var b strings.Builder
	b.WriteString("^XA\n")
	b.WriteString("^CI28\n") // UTF-8 input encoding
	b.WriteString("^PW812\n^LL1218\n")

	b.WriteString(fmt.Sprintf("^FO20,20^A0N,40,40^FD%s^FS\n", zplEscape(l.CarrierCode)))
	b.WriteString(fmt.Sprintf("^FO20,70^A0N,28,28^FD%s (%s)^FS\n",
		zplEscape(l.ServiceName), zplEscape(l.ServiceMode)))
	b.WriteString("^FO20,110^GB772,3,3^FS\n")

	b.WriteString(fmt.Sprintf("^FO20,130^BY3^BCN,140,Y,N,N^FD%s^FS\n", zplEscape(l.AWB)))

	b.WriteString(fmt.Sprintf("^FO20,310^A0N,60,60^FD%s^FS\n", zplEscape(l.RoutingCode)))
	b.WriteString("^FO20,380^GB772,3,3^FS\n")

	b.WriteString("^FO20,400^A0N,24,24^FDFROM^FS\n")
	writeZPLParty(&b, 430, l.Sender)
	b.WriteString("^FO20,560^GB772,3,3^FS\n")

	b.WriteString("^FO20,580^A0N,28,28^FDTO^FS\n")
	writeZPLParty(&b, 615, l.Recipient)

	b.WriteString("^FO20,790^GB772,3,3^FS\n")
	b.WriteString(fmt.Sprintf("^FO20,810^A0N,28,28^FDPieces: %d   Weight: %s^FS\n",
		l.PieceCount, zplEscape(l.WeightLabel)))
	b.WriteString(fmt.Sprintf("^FO20,850^A0N,28,28^FDPayment: %s^FS\n", zplEscape(l.PaymentMode)))
	if l.CODAmountLabel != "" {
		b.WriteString(fmt.Sprintf("^FO20,890^A0N,44,44^FDCOD %s^FS\n", zplEscape(l.CODAmountLabel)))
	}
	if l.IsFragile {
		b.WriteString("^FO500,890^A0N,44,44^FDFRAGILE^FS\n")
	}

	b.WriteString(fmt.Sprintf("^FO560,950^BQN,2,6^FDLA,%s^FS\n", zplEscape(l.QRPayload)))
	b.WriteString(fmt.Sprintf("^FO20,960^A0N,24,24^FDBooked: %s^FS\n",
		l.BookedAt.Format("2006-01-02 15:04")))
	if l.PromisedDeliveryAt != nil {
		b.WriteString(fmt.Sprintf("^FO20,995^A0N,24,24^FDPromised: %s^FS\n",
			l.PromisedDeliveryAt.Format("2006-01-02")))
	}
	if l.ReferenceNumber != "" {
		b.WriteString(fmt.Sprintf("^FO20,1030^A0N,24,24^FDRef: %s^FS\n", zplEscape(l.ReferenceNumber)))
	}
	b.WriteString("^XZ\n")
	return b.String()
}

func writeZPLParty(b *strings.Builder, y int, p LabelParty) {
	lines := nonEmpty(
		strings.TrimSpace(p.Name+" "+p.Phone),
		p.Company,
		p.Line1,
		strings.TrimSpace(p.Line2+" "+p.Landmark),
		fmt.Sprintf("%s, %s - %s", p.City, p.State, p.Pincode),
	)
	for i, line := range lines {
		b.WriteString(fmt.Sprintf("^FO20,%d^A0N,26,26^FD%s^FS\n", y+i*30, zplEscape(truncate(line, 48))))
	}
}

// zplEscape neutralises the ZPL control characters so a customer-supplied name
// cannot inject label commands.
func zplEscape(s string) string {
	r := strings.NewReplacer("^", " ", "~", " ", "\\", " ", "\n", " ", "\r", " ")
	return r.Replace(s)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func formatWeight(grams int32) string {
	if grams < 1000 {
		return fmt.Sprintf("%d g", grams)
	}
	return fmt.Sprintf("%s kg", money.FormatMinor(int64(grams), 3))
}

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
