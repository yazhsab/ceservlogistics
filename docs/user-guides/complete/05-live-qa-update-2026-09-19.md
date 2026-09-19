# Product walkthrough update — 19 September 2026

Read this with the four full-product manuals dated 18 September. These instructions supersede their descriptions of the corrected flows below. The QA report and screenshot gallery distinguish live checks from remaining acceptance work.

## Create a customer and book

1. Authorized staff open **Commercial → Customers → Add customer**, or **New booking → Sender → Add customer**.
2. Enter the customer's name/contact details, choose the type and save. The system generates the code.
3. In booking, search by code, name, email or phone and **select a result**. Typing an arbitrary code does not select a customer.
4. Select a saved pickup address or enter a one-off address. Capture recipient country and actual postcode. Both ends require configured service coverage.
5. Enter packages, service, payment and payer details, preview route/charges, then confirm booking.

A customer record and login are separate. The record holds shipments/billing; an administrator creates and links a login for portal access. Customers now land in their portal; eligible franchise users land in the franchise dashboard. Missing actions can reflect role or scope.

## Customs and insurance

Enter goods quantity and unit value from the customer's invoice. Goods totals, discount, declared value and invoice total update from the server. Unit prices remain inputs; the system cannot infer an item's selling price.

Example: two items at NGN 1,000 give NGN 2,000 goods value. NGN 100 discount gives NGN 1,900 declared value. Add NGN 250 invoice freight and NGN 50 other charges for NGN 2,200 before insurance. This commercial-invoice amount is separate from the courier quote.

Insurance uses the configured rate-card rule, with no assumed 1% fallback. Ask whether the customer wants insurance, show the premium and record acceptance. The QA example used 1.25% and NGN 25 premium on NGN 2,000 declared value; this is a test tariff, not an approved business rate.

If a postcode is not configured, record both countries and exact postcodes. Ask the network administrator to check geography, coverage, route and tariff. Do not substitute an unrelated postcode to force booking.

## COD delivery and custody

1. Open the assigned delivery run and stop. Record outcome and exact collected amount; the backend validates required COD.
2. After delivery, a user with COD collection permission sees **Record COD custody**. Select the actual method and its reference when required.
3. Choose **Record COD collection**. The success message confirms custody; then follow branch cash handover procedures.
4. On network failure, **Retry COD recording** reuses the same details/request identifier. A prior record shows **COD already recorded** instead of creating another.

Delivery and custody recording are separate steps. Finance can inspect the obligation and use **Check custody** to compare holdings with the ledger. The QA example matched NGN 1,000 in both; no real money moved.

## Credit-note checker handoff

1. The maker opens the invoice and creates a draft with correction reason/amount.
2. A different authorized checker signs in, opens **Finance → Billing**, and opens the same invoice.
3. In **Credit notes**, choose **Review credit note**, verify details and issue when appropriate.
4. The note receives its final number and credited amount updates. The maker cannot approve their own draft.

Drafts now remain discoverable from the invoice after changing accounts. This replaces the earlier manual's checker-retrieval limitation. Zero settlements now say **No payment due** and offer no payment action.

## Operations and returns

Confirm the operating facility before scanning. Errors remain visible; read-only roles see history without submission controls. Account changes clear the previous facility.

Multi-facility operators select the actual arrival stop for a trip. Failed delivery enters NDR. Authorized RTO begins the reverse journey; record each facility receipt and sender handover. The last leg is labeled **Sender**.

## Remaining setup and acceptance

Live checks covered insured prepaid delivery, COD delivery/custody, NDR/RTO and selected invoice/settlement flows. POD needs browser file permission; e-commerce testing needs a configured store. Complete COD handover/remittance, nonzero settlements, forward hub sorting and responsive/accessibility acceptance remain open.

Train only with marked QA accounts and synthetic records. Sixteen test users remain active until acceptance finishes; use the account-lifecycle instructions to deactivate afterward.
