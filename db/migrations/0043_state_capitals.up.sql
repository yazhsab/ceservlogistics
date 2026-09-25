-- 0043 Add canonical state-capital reference data for address entry.
--
-- Capitals are global geography metadata. Booking clients may use them as a
-- convenient default, while the actual postal code remains user-selected and
-- continues to drive serviceability and pricing.

ALTER TABLE states
    ADD COLUMN capital_city text,
    ADD COLUMN capital_pincode text CHECK (capital_pincode IS NULL OR capital_pincode ~ '^[A-Z0-9 -]{3,12}$');

WITH nigeria_capitals(state_code, capital_city, capital_pincode) AS (
VALUES
    ('AB', 'Umuahia', '440221'),
    ('AD', 'Yola', '640101'),
    ('AK', 'Uyo', '520211'),
    ('AN', 'Awka', '420102'),
    ('BA', 'Bauchi', '740101'),
    ('BY', 'Yenagoa', '560212'),
    ('BE', 'Makurdi', '970101'),
    ('BO', 'Maiduguri', '600001'),
    ('CR', 'Calabar', '540211'),
    ('DE', 'Asaba', '320211'),
    ('EB', 'Abakaliki', '480211'),
    ('ED', 'Benin City', '300001'),
    ('EK', 'Ado Ekiti', '360211'),
    ('EN', 'Enugu', '400001'),
    ('FC', 'Abuja', '900001'),
    ('GO', 'Gombe', '760211'),
    ('IM', 'Owerri', '460211'),
    ('JI', 'Dutse', '720211'),
    ('KD', 'Kaduna', '800001'),
    ('KN', 'Kano', '700001'),
    ('KT', 'Katsina', '820001'),
    ('KE', 'Birnin Kebbi', '860101'),
    ('KO', 'Lokoja', '260101'),
    ('KW', 'Ilorin', '240005'),
    ('LA', 'Ikeja', '100001'),
    ('NA', 'Lafia', '950101'),
    ('NI', 'Minna', '920211'),
    ('OG', 'Abeokuta', '110101'),
    ('ON', 'Akure', '340211'),
    ('OS', 'Oshogbo', '230211'),
    ('OY', 'Ibadan', '200001'),
    ('PL', 'Jos', '930105'),
    ('RI', 'Port Harcourt', '500001'),
    ('SO', 'Sokoto', '840101'),
    ('TA', 'Jalingo', '660211'),
    ('YO', 'Damaturu', '620212'),
    ('ZA', 'Gusau', '860241')
)
UPDATE states s
SET capital_city = n.capital_city,
    capital_pincode = n.capital_pincode
FROM nigeria_capitals n
JOIN countries c ON c.iso2 = 'NG'
WHERE s.country_id = c.id AND s.code = n.state_code;
