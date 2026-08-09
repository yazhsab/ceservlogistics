-- 0031 Nigeria as a supported market.
--
-- The platform was built for India and its assumptions leaked into three
-- places: a tax-type vocabulary that only names Indian taxes, a country table
-- with one row in it, and a tax-ID field pair called GSTIN and PAN.
--
-- What did *not* need changing is worth recording, because it was the fear:
-- the pricing engine's tax computation is already country-agnostic. It lists
-- applicable rules and applies each one's basis points; the intra-state/
-- inter-state split is expressed as a nullable `intra_state_only` column, and a
-- rule that leaves it NULL applies either way. A Nigerian VAT rule is therefore
-- ordinary configuration, not a code change.
--
-- Nigerian postal codes are also six digits not starting with zero (Lagos
-- 100001, Abuja 900001), so the existing validation happens to fit. Whether
-- Nigerian senders reliably *know* their postcode is a product question and is
-- recorded in the release notes rather than solved here.

-- ---------------------------------------------------------------------------
-- Tax vocabulary
--
-- 'CUSTOM' already existed as an escape hatch and a Nigerian rule could have
-- used it, but a VAT line reading "CUSTOM 7.5%" on a customer's invoice is
-- the kind of small dishonesty that erodes trust in a document. Name it.
-- ---------------------------------------------------------------------------
ALTER TABLE tax_rules DROP CONSTRAINT tax_rules_tax_type_check;
ALTER TABLE tax_rules ADD CONSTRAINT tax_rules_tax_type_check
    CHECK (tax_type IN (
        -- India
        'CGST','SGST','IGST','UTGST','CESS',
        -- Nigeria and most single-rate jurisdictions
        'VAT',
        -- Withholding, which Nigeria applies to some services
        'WHT',
        'CUSTOM'));

-- ---------------------------------------------------------------------------
-- Nigeria
-- ---------------------------------------------------------------------------
INSERT INTO countries (public_id, iso2, iso3, name, phone_code, currency, status)
VALUES (gen_seed_public_id('cnt'), 'NG', 'NGA', 'Nigeria', '+234', 'NGN', 'ACTIVE')
ON CONFLICT (iso2) DO NOTHING;

-- The 36 states plus the Federal Capital Territory. Seeded because
-- serviceability, zone mapping and address validation all resolve through
-- states, and a market with no states cannot take a booking.
INSERT INTO states (public_id, country_id, code, name, status)
SELECT gen_seed_public_id('stt'), c.id, s.code, s.name, 'ACTIVE'
FROM countries c
CROSS JOIN (VALUES
    ('AB','Abia'),('AD','Adamawa'),('AK','Akwa Ibom'),('AN','Anambra'),
    ('BA','Bauchi'),('BY','Bayelsa'),('BE','Benue'),('BO','Borno'),
    ('CR','Cross River'),('DE','Delta'),('EB','Ebonyi'),('ED','Edo'),
    ('EK','Ekiti'),('EN','Enugu'),('FC','Federal Capital Territory'),
    ('GO','Gombe'),('IM','Imo'),('JI','Jigawa'),('KD','Kaduna'),('KN','Kano'),
    ('KT','Katsina'),('KE','Kebbi'),('KO','Kogi'),('KW','Kwara'),('LA','Lagos'),
    ('NA','Nasarawa'),('NI','Niger'),('OG','Ogun'),('ON','Ondo'),('OS','Osun'),
    ('OY','Oyo'),('PL','Plateau'),('RI','Rivers'),('SO','Sokoto'),
    ('TA','Taraba'),('YO','Yobe'),('ZA','Zamfara')
) AS s(code, name)
WHERE c.iso2 = 'NG'
ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- The organization's country
--
-- Organizations carried a currency and a timezone but not a country, which is
-- what decides how a tax identifier is validated, what a postcode looks like
-- and which label a UI should show. It defaulted to India by being the only
-- market; now it has to be said out loud.
--
-- Existing rows are backfilled to IN because that is what they are. New
-- organizations must state it.
-- ---------------------------------------------------------------------------
ALTER TABLE organizations
    ADD COLUMN country char(2) NOT NULL DEFAULT 'IN'
        CHECK (country ~ '^[A-Z]{2}$');

COMMENT ON COLUMN organizations.country IS
    'ISO-3166 alpha-2. Decides tax identifier validation and market-specific labels.';

-- ---------------------------------------------------------------------------
-- Tax identifiers
--
-- gst_number and pan_number are Indian names for a general idea: the
-- registration a counterparty is invoiced under. Nigeria has a TIN and a CAC
-- registration number, and a Nigerian operator being asked for a "GSTIN" will
-- either leave it blank or put the wrong thing in it.
--
-- Renaming the columns would break every query and every client. Instead the
-- pair is generalised in place: tax_registration_number and
-- business_registration_number are added, the old columns are kept in sync for
-- existing readers, and the API prefers the new names.
-- ---------------------------------------------------------------------------
ALTER TABLE franchises
    ADD COLUMN tax_registration_number      text,
    ADD COLUMN business_registration_number text;

ALTER TABLE customers
    ADD COLUMN tax_registration_number      text,
    ADD COLUMN business_registration_number text;

-- Carry across what is already recorded, so nothing is lost and a reader of
-- the new column sees the same value.
UPDATE franchises SET tax_registration_number = gst_number
 WHERE gst_number IS NOT NULL AND tax_registration_number IS NULL;
UPDATE franchises SET business_registration_number = pan_number
 WHERE pan_number IS NOT NULL AND business_registration_number IS NULL;
UPDATE customers SET tax_registration_number = gst_number
 WHERE gst_number IS NOT NULL AND tax_registration_number IS NULL;
UPDATE customers SET business_registration_number = pan_number
 WHERE pan_number IS NOT NULL AND business_registration_number IS NULL;

COMMENT ON COLUMN franchises.tax_registration_number IS
    'The registration this counterparty is invoiced under: GSTIN in India, TIN in Nigeria. Validated per the organization''s country.';
COMMENT ON COLUMN franchises.business_registration_number IS
    'Company registration: PAN in India, CAC/RC number in Nigeria.';
COMMENT ON COLUMN franchises.gst_number IS
    'Deprecated: use tax_registration_number. Kept for existing readers.';
COMMENT ON COLUMN franchises.pan_number IS
    'Deprecated: use business_registration_number. Kept for existing readers.';

-- ---------------------------------------------------------------------------
-- The provider handle a message was composed against
--
-- notification_templates.provider_ref has existed since 0026 — an approved
-- WhatsApp template name, a registered SMS sender id — and Message.ProviderRef
-- has existed in the adapter contract to carry it. Nothing joined them: the
-- value was never read, so no provider that needs one could work.
--
-- Snapshotted onto the notification rather than read from the template at send
-- time, for the same reason the shipment carries a price snapshot: a template
-- edited or re-approved next month must not change what a message sent today
-- was addressed with.
-- ---------------------------------------------------------------------------
ALTER TABLE notifications ADD COLUMN provider_ref text;

COMMENT ON COLUMN notifications.provider_ref IS
    'The template''s provider handle at composition time. WhatsApp template name, SMS sender id. Snapshot: never re-read from the template.';
