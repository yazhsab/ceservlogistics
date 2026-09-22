# CESERV Common Questions and Quick Reference

For end users and the teams helping them

Start with the question that matches the screen in front of you. This guide explains common customer, booking, pricing and operational problems. The most frequent customer-field issue is that the operator has typed a name but has not selected a saved account from the search results.

**Edition:** 18 September 2026. Examples are illustrative. Your permissions, account configuration and shipment status determine which actions are available.

### What is the customer code

It is the reference assigned to a saved customer account. Newly created codes begin with **CUS-**. Open **Commercial → Customers** to find the customer's code. Existing accounts may use older code formats; use the code actually shown on the record.

### Nothing I type in Customer is accepted

The field is a lookup, not a free-text customer name. Type at least two characters of the customer's name, code, email or phone, wait for the results and **click the matching result**. Confirm that the selected name and code appear below the field. Editing the search text clears the selection.

### How do I create a customer

Open **Commercial → Customers → Add customer**. Select **RETAIL** or **BUSINESS** and enter the name and phone. Business accounts also need an **Account code** and **Legal name**. Select **Create customer**, then return to booking and search for the new account. The Getting Started and Booking Guide shows each step.

### Why is there no customer in the results

Check spelling, try the saved phone or email, and verify the record under **Customers**. Booking searches active customers in your authorized organization. A suspended, closed or different organization's customer will not be available. Search before creating a duplicate.

### Why can I not see Add customer or another menu

Your role may permit viewing without creating, or restrict access to a branch. Ask the administrator to review the relevant permission and operating scope. Another person's credentials are not a workaround.

### Where are the other answers

Page 2 covers logins, identifiers and addresses. Page 3 covers prices, insurance and customs. Page 4 covers scans, delivery and money. Page 5 explains statuses and what to send support.

<!-- page -->
## Accounts identifiers and addresses

### Does creating a customer give them a password

No. The commercial record is separate from a user login. Portal access requires a provisioned customer user linked to that account. Ask the administrator to arrange it. The current staff **Add user** form does not itself make that customer link.

### Where should staff and customers sign in

Staff use **https://app.ceservlogistics.com/login**. Provisioned customers use **https://customer.ceservlogistics.com/login**. An ordinary staff login is not a customer portal login. The portal provides Overview, Shipments, Invoices and Profile; customers contact the service team for bookings or address changes.

### I forgot my password

Use **Forgot password?** on the sign-in page and follow the reset instructions. If no usable reset message arrives, check the email used and ask the administrator for help; message delivery depends on the configured service. A new password must meet the rules shown, including at least 12 characters. Never send your password or reset link in a support screenshot.

### Which reference belongs in this field

| Reference | Use |
| --- | --- |
| Customer code | Find the saved account; newly generated codes begin CUS- |
| Business Account code | Commercial account reference supplied when creating a business customer |
| AWB | Identify, scan or publicly track a shipment |
| Customer ID beginning cus_ | Use only when a form specifically requests Customer ID |
| Shipment ID beginning shp_ | Use in forms specifically requesting Shipment ID |
| Operating unit ID beginning ou_ | Identify the facility in forms requesting a facility ID |

Do not guess IDs or replace them with an account name or AWB. Ask the administrator for the correct record reference when the screen offers no lookup.

### Why is the postal code rejected

Select the correct country first. Nigeria and India require six digits starting from 1 to 9. UK codes allow letters and numbers and are normalized, for example **SS07JJ** becomes **SS0 7JJ**. Other supported countries accept their configured format; do not force a foreign code into a domestic saved-address field.

### The address is valid so why is it not serviceable

Format validation and service coverage are different checks. The country, postal area, courier product, route and applicable prices must all be configured. Confirm the address, then ask the network or pricing administrator to review the returned reason. Selecting a country does not activate delivery to it.

<!-- page -->
## Prices insurance and customs

### Why is the price higher than the weight I entered

The chargeable weight can reflect dimensions, minimum chargeable weight and configured rounding as well as actual weight. Check the measurements and their units, then read the preview's charge breakdown. Goods value, COD, surcharges, tax and transport charges are separate amounts.

### Is insurance always 1 percent

No. An administrator configures insurance rules on the applicable rate-card version. The rule can use a percentage, fixed charge or per-kilogram charge with restrictions or limits. The current preview gives the actual premium. There is no automatic 1% fallback when insurance is unconfigured.

### Why are there two insurance checkboxes

**Customer requests shipment insurance** asks the system to include an insurance quote. After preview, the acceptance checkbox records that the customer accepts the current quoted premium. Requesting insurance alone does not record that agreement.

### Why did the acceptance clear after I changed a field

The quote may have changed. Refresh the preview, explain the current premium and obtain the customer's acceptance again. If the customer declines, clear the insurance request and refresh the preview.

### What does insurance not configured mean

Ask the pricing administrator to check the active rate-card version, effective dates, customer assignment, product eligibility, declared value and matching insurance rules. A product permitting insurance is not enough to calculate a premium. Do not tell the customer that the parcel is insured after a failed quote.

### Why does the customs total differ from the shipment price

The customs total values the goods and associated customs charges. The shipment quote prices transportation and other shipment charges. Goods subtotal less discount gives declared goods value; customs freight, quoted insurance and other customs charges then contribute to the customs invoice total. Neither amount should be copied over the other to make them match.

### Can different people pay transport and duty

Yes. Set **Bill transportation to** and **Bill duty and tax to** independently to Shipper, Receiver or Third party. Select an authorized active billing account when required. Third-party billing always needs an account; receiver transportation on credit needs one too.

### Does the customs form file the declaration or pay duty

No. It saves goods, valuation and payer instructions. It does not automatically file with customs, calculate or collect import duty, exchange currencies or issue an insurer's certificate. Follow the authorized customs and insurance process alongside the booking record.

<!-- page -->
## Scanning delivery and finance questions

### A scan types the code but does not submit

Focus the scan input and press **Enter**. Configure the scanner with an Enter suffix. There is no scanner pairing wizard in the application. Test typing in a text editor if necessary; do not make test operational scans against real parcels.

### Why is the scan or dispatch rejected

Check the exact error, current facility, selected operation, existing container or run, and shipment status. A held parcel, wrong destination, missing custody step or duplicate active run can prevent the action. Do not change modes randomly to force acceptance.

### The trip arrived so why are parcels not received

Trip arrival records the vehicle or transport movement. Receive the manifest and bags in the correct facility and reconcile the actual parcels. A container receipt also does not certify that its expected and actual contents agree.

### I added the stop so why is it not out for delivery

Adding stops builds the run. **Dispatch run** makes eligible shipments out for delivery. The run must have the appropriate branch custody, assignment and state before it can be dispatched.

### The booking page froze after I confirmed

Wait for a clear response. If the connection failed, search **Shipments** and check the customer or reference before starting another booking. Record the time and error for support. A second newly entered booking can create a duplicate even if the first response was lost.

### The customer paid so why does finance show money outstanding

Booking, a payment record and a cash handover are separate events. Check whether the payment belongs in customer collections, COD or invoice payments, whether it was saved against the correct reference, and whether the next cash holder accepted the handover. Ask finance to investigate before recording the same receipt twice.

### Can I change an issued invoice or closed bag

These records preserve an approved or physical declaration. Use the authorized adjustment or exception workflow. Issued invoices use permitted credit or debit notes, and closed container corrections require supervision. The current billing screen does not provide invoice PDF rendering.

### Why can I not download the report

It may still be queued or running, may have failed, or its download may have expired. Open the report run and read the status. Completed reports have a 24-hour download window; rerun an expired report with the correct filters and permissions.

<!-- page -->
## Read statuses and ask for help

### Common terms

| Term | Meaning for the operator |
| --- | --- |
| Booked | A shipment record and AWB exist; physical collection may still be pending |
| In transit | The parcel is moving through the network; check the latest event and facility |
| OFD | Out for delivery on a dispatched delivery run |
| Delivered | A successful delivery outcome has been recorded |
| NDR | Non-delivery report requiring a next action after a failed attempt |
| RTO | Return to origin, including the reverse movement and sender handover |
| POD | Proof of delivery such as a signature, photo or other supported evidence |
| COD | Cash on delivery amount and its collection and custody record |
| Declared value | Value of the goods, separate from freight and the COD amount |
| Active | An account or configuration is enabled; other eligibility checks can still apply |

Read the full record and timeline before deciding what happened. A status badge is a summary, not a substitute for the event, party, amount and time behind it.

### Send support the information needed to act

1. **Screen and action:** name the menu and button you used and what you expected to happen.
2. **Reference:** include the relevant customer code, AWB, bag, manifest, trip, run, invoice or report reference.
3. **Context:** give the branch or hub, date and time, selected product or operation, and the affected user's work email if authorized.
4. **Result:** copy the exact error text or code. Attach a screenshot of the relevant area with unrelated personal information hidden.
5. **Last confirmed step:** explain what succeeded before the error and whether a saved record, payment or physical handover might already exist.

Send the details to your designated supervisor or support contact. Keep passwords, OTPs, reset links, access tokens and payment card details out of the request. Do not repeat a booking or financial action merely to obtain another screenshot.

### Choose the right person

Account selection and customer setup usually go to customer service or the administrator. Missing permissions go to the administrator. Coverage and route failures go to the network team; rate or insurance setup goes to pricing. Cash, invoices and settlements go to finance. Parcel, seal and delivery exceptions go to the operations supervisor.

For the complete steps, use the Getting Started and Booking Guide, Administrator Configuration Guide or Daily Operations Guide supplied with this reference.
