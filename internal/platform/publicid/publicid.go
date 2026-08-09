// Package publicid generates opaque, non-sequential, prefixed public identifiers
// for external API surfaces.
//
// Internal primary keys are bigint and MUST NOT be exposed. Every externally
// visible object is addressed by a public ID of the shape:
//
//	<prefix>_<26 char Crockford base32 ULID>
//
// e.g. shp_01J9ZC8Q4H7K2M3N4P5Q6R7S8T
//
// The encoded value is a 128-bit ULID: 48 bits of millisecond timestamp followed
// by 80 bits of cryptographically random entropy. This keeps identifiers roughly
// time-ordered (good for index locality) while remaining unguessable: an attacker
// cannot enumerate objects by incrementing an ID.
package publicid

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"strings"
	"time"
)

// Entity prefixes. Keep this list authoritative — the OpenAPI contract documents
// these prefixes and the frontend may rely on them for routing/telemetry.
const (
	PrefixOrganization      = "org"
	PrefixUser              = "usr"
	PrefixRole              = "rol"
	PrefixPermission        = "perm"
	PrefixSession           = "ses"
	PrefixAuditEvent        = "aud"
	PrefixRegion            = "rgn"
	PrefixOperatingUnit     = "ou"
	PrefixFranchise         = "frn"
	PrefixAgreement         = "agr"
	PrefixCountry           = "cnt"
	PrefixState             = "stt"
	PrefixDistrict          = "dst"
	PrefixCity              = "cty"
	PrefixPincode           = "pin"
	PrefixLocality          = "loc"
	PrefixZone              = "zn"
	PrefixZoneMapping       = "zmp"
	PrefixServiceArea       = "sva"
	PrefixServiceabilityRul = "svr"
	PrefixRoute             = "rte"
	PrefixRouteLeg          = "rtl"
	PrefixRoutingOverride   = "rov"
	PrefixTemporaryClosure  = "tcl"
	PrefixCourierService    = "svc"
	PrefixRateCard          = "rc"
	PrefixRateCardVersion   = "rcv"
	PrefixWeightSlab        = "wsl"
	PrefixZoneRate          = "zrt"
	PrefixSurchargeRule     = "sur"
	PrefixDiscountRule      = "dsc"
	PrefixTaxRule           = "tax"
	PrefixCustomer          = "cus"
	PrefixBusinessAccount   = "bac"
	PrefixCustomerContact   = "cct"
	PrefixCustomerAddress   = "adr"
	PrefixBillingProfile    = "bpr"
	PrefixCreditProfile     = "cpr"
	PrefixShipment          = "shp"
	PrefixPackage           = "pkg"
	PrefixShipmentEvent     = "evt"
	PrefixJob               = "job"
	PrefixImportJob         = "imp"
	PrefixExplanation       = "exp"
	PrefixSettlement        = "stl"
	PrefixInvoice           = "inv"

	// Release 2 — physical operations.
	PrefixScanEvent        = "scn"
	PrefixPickupRequest    = "pkr"
	PrefixPickupRun        = "prn"
	PrefixPickupAssignment = "pas"
	PrefixPickupAttempt    = "pat"
	PrefixBag              = "bag"
	PrefixBagEvent         = "bev"
	PrefixManifest         = "mft"
	PrefixManifestEvent    = "mev"
	PrefixCarrier          = "car"
	PrefixVehicle          = "veh"
	PrefixDriver           = "drv"
	PrefixTrip             = "trp"
	PrefixTripLeg          = "tlg"
	PrefixTripAssignment   = "tas"
	PrefixTripEvent        = "tev"
	PrefixException        = "exc"
	PrefixReconciliation   = "rec"
	PrefixHold             = "hld"
	PrefixDeliveryRun      = "drn"
	PrefixDeliveryRunItem  = "dri"
	PrefixDeliveryAttempt  = "dat"
	PrefixNDRReason        = "ndrs"
	PrefixNDRCase          = "ndr"
	PrefixNDRAttempt       = "nat"
	PrefixNDRAction        = "nac"
	PrefixRTOCase          = "rto"
	PrefixStoredObject     = "obj"
	PrefixProofOfDelivery  = "pod"
	PrefixPODArtifact      = "pda"
)

// crockford is Crockford base32: no I, L, O, U — avoids transcription errors.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var decodeMap [256]int8

func init() {
	for i := range decodeMap {
		decodeMap[i] = -1
	}
	for i, c := range crockford {
		decodeMap[c] = int8(i)
		decodeMap[strings.ToLower(string(c))[0]] = int8(i)
	}
	// Crockford decoding aliases for visually similar characters.
	for _, a := range []struct {
		c byte
		v int8
	}{{'i', 1}, {'I', 1}, {'l', 1}, {'L', 1}, {'o', 0}, {'O', 0}} {
		decodeMap[a.c] = a.v
	}
}

// ErrInvalid is returned when a public ID is malformed or has the wrong prefix.
var ErrInvalid = errors.New("invalid public id")

// New returns a new public ID with the supplied prefix.
func New(prefix string) string {
	return newAt(prefix, time.Now())
}

func newAt(prefix string, t time.Time) string {
	var raw [16]byte
	ms := uint64(t.UTC().UnixMilli())
	raw[0] = byte(ms >> 40)
	raw[1] = byte(ms >> 32)
	raw[2] = byte(ms >> 24)
	raw[3] = byte(ms >> 16)
	raw[4] = byte(ms >> 8)
	raw[5] = byte(ms)
	// crypto/rand.Read never fails on supported platforms; it panics internally
	// on catastrophic entropy failure, which is the correct behaviour here.
	if _, err := rand.Read(raw[6:]); err != nil {
		panic("publicid: entropy source failure: " + err.Error())
	}
	return prefix + "_" + encode(raw)
}

// encode renders 16 bytes as 26 Crockford base32 characters (130 bits of space,
// the leading 2 bits are always zero).
func encode(raw [16]byte) string {
	hi := binary.BigEndian.Uint64(raw[0:8])
	lo := binary.BigEndian.Uint64(raw[8:16])
	out := make([]byte, 26)
	for i := 25; i >= 0; i-- {
		out[i] = crockford[lo&0x1f]
		lo = (lo >> 5) | (hi << 59)
		hi >>= 5
	}
	return string(out)
}

// Parse validates that id is well formed and carries the expected prefix.
// It returns the raw suffix on success. Callers use this to reject malformed
// identifiers before they ever reach the database layer.
func Parse(expectedPrefix, id string) (string, error) {
	prefix, suffix, ok := strings.Cut(id, "_")
	if !ok || prefix != expectedPrefix {
		return "", ErrInvalid
	}
	if len(suffix) != 26 {
		return "", ErrInvalid
	}
	for i := 0; i < len(suffix); i++ {
		if decodeMap[suffix[i]] < 0 {
			return "", ErrInvalid
		}
	}
	return suffix, nil
}

// Valid reports whether id is a well-formed public ID with the expected prefix.
func Valid(expectedPrefix, id string) bool {
	_, err := Parse(expectedPrefix, id)
	return err == nil
}

// Prefix returns the prefix portion of a public ID, or "" when malformed.
func Prefix(id string) string {
	prefix, _, ok := strings.Cut(id, "_")
	if !ok {
		return ""
	}
	return prefix
}
