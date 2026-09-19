# CESERVE Configuration and Administration

For administrators network managers commercial managers and integration owners

This manual covers the setup needed across the whole product: organization identity, user access, the operating network, geography, routing, services, package presets, customer accounts, rate cards, insurance, notifications and partner connections. Finance rule configuration is covered in the Operations Finance and Reporting manual alongside its verification workflow.

**Edition:** 18 September 2026. Use approved business settings. Configuration examples are not company prices, tax advice or insurance policy terms. Staff access is at https://app.ceservlogistics.com/login.

### Configure in dependency order

| Setup stage | Page |
| --- | --- |
| Organization identity and administration | 2 |
| Users roles and operating scope | 3 |
| Facilities geography coverage and routing | 4 |
| Products package presets and pricing masters | 5 |
| Rate cards and configurable insurance | 6 to 7 |
| Customer accounts and readiness checks | 8 |
| Notification templates and delivery operations | 9 to 10 |
| Partner API credentials and webhooks | 11 to 12 |
| Merchant connections and release readiness | 13 to 14 |

Prepare the approved facility hierarchy, postal coverage, product definitions, parcel limits, routing arrangements, rate schedules and commercial account terms before entering configuration. Identify who may publish prices, grant access, accept money and approve financial documents.

**Validate the whole chain:** an active product alone does not create coverage or rates. A customer record alone does not provide credit or portal access. A template alone does not deliver a notification. After setup, verify representative workflows with the people who will use them in an approved training environment.

**Use the product as configured:** some fields require record IDs instead of display codes. Keep a controlled reference list where lookup controls are unavailable. If a capability has no available administration screen, ask implementation support rather than substituting unrelated records.

<!-- page -->
## Maintain organization identity

**Where:** Administration → Organization. This screen identifies the organization whose records staff are using and shows its shipment numbering context.

1. Review the display name, legal name, organization code, AWB prefix, currency, timezone, contact details and tax registration number.
2. If authorized, select **Edit organization**. Update the display name, legal name, timezone, contact email, contact phone or TIN as appropriate.
3. Use the approved timezone identifier, such as **Africa/Lagos**, rather than a casual description such as local time. Check the legal and tax details against the organization's records.
4. Select **Save changes**, wait for confirmation and review the displayed values again. Brief affected staff when an identity or timezone change affects their work.

**Fixed settings:** code, currency, AWB prefix and status are not editable through this form. Contact implementation support for a required change. Do not create a second organization merely to change a label or numbering setting.

### Establish administration ownership

Assign named owners for access, network coverage, pricing, customer accounts, finance configuration, message templates and external connections. Keep a record of approved values and effective dates outside the application where no change-history screen is offered.

This screen is not a server, domain, backup, deployment or branding editor. Product hosting, notification provider credentials, merchant mapping and other deployment settings require the responsible technical administrator. Do not enter server credentials into contact or notes fields.

### Review access changes through Users

On a staff member's detail page, use the permitted **Edit profile**, role grant or removal, and activation or deactivation actions. Identity, status and access assignments are separate controls. Check all existing assignments when moving someone to another branch.

After an authorized change, ask the user to verify the intended workspace and access scope. If the new scope is not reflected, sign out and back in before escalating. Deactivating a user is an account control, not a reassignment of that person's pickups, delivery runs or cash responsibility; managers must arrange those handovers separately.

**Record to retain:** the person, intended role and facility, approved change, effective time and outstanding operational handover. Use individual accounts so recorded actions remain attributable.

<!-- page -->
## Give each person the correct access

**Where:** Administration → Users; Administration → Roles & permissions.

1. Open **Users** and select **Add user**.
2. Enter the staff member's full name and work email. Add a phone number if appropriate.
3. Provide a temporary password of at least 12 characters through your organization's approved private channel. The new user is normally required to change it at first sign-in.
4. Select **Initial role**. If that role requires a scope, select the correct **Operating unit**. Save the user.
5. Have the staff member sign in and check **My profile**, the visible menu and the actions required for the job. Confirm that they see the correct facility's work.

### Assign access by responsibility

| Responsibility | Access to review with the administrator |
| --- | --- |
| Counter staff | Customer search or creation, shipment preview and booking, label viewing |
| Dispatch and hub staff | Relevant facility, pickup or delivery runs, scanning, bags, manifests and trips |
| Finance staff | Assigned collections, COD, billing, ledger or settlement functions |
| Configuration manager | Products, coverage, pricing and the authority to activate a rate version |
| Customer portal user | Customer portal permission and an explicit link to the correct customer account |

These are responsibilities, not promises that a particular role name grants every listed action. Review the effective permission list. A role may permit viewing but not creating, approving or publishing.

### Maintain roles and accounts

Review **Roles & permissions** before creating a custom role. System roles are protected. Custom role creation and permission assignment are restricted to what your administrator account may grant.

Use the user detail actions to manage access changes. When someone changes branch or leaves, review their role, operating scope and account status promptly. Never use shared staff credentials to get past a missing button.

**Customer portal access is separate:** a commercial customer record has no password by itself. The customer login must be provisioned and linked to that account. The normal **Add user** staff form does not complete that commercial link. Refer portal provisioning to the system administrator or implementation support when no linking control is available.

<!-- page -->
## Configure facilities coverage and routes

### Build the operating network

1. Use **Network** to create or review hubs, branches and franchise facilities within the approved parent hierarchy. Work from the parent facility down to its children; use support for hierarchy levels not exposed by these screens.
2. Check the facility **Code**, **Name**, **Type**, parent and address before saving. Facility codes and types are fixed identities; do not create duplicates to correct an address.
3. Give staff access to the correct operating units. For franchise operations, check the franchise relationship to its active branch as well as user access.

**If a form asks for an ID:** use the actual record ID supplied by your administrator. An operating-unit ID is different from the visible branch code. Keep an approved reference list for the operators who need these fields.

### Establish postal coverage and pricing zones

4. Open the **Postal code master** and search representative collection and delivery areas. Confirm that the required areas exist and belong to the intended locations.
5. Review **Pricing zones**. Create a zone with its code, name, type and sort order when needed. Lane prices refer to these zone codes, so coordinate changes with pricing staff.
6. Use **Bulk import** only with the supported import type and format shown by the application. Inspect accepted and rejected rows and fix the source before retrying rejected records.
7. Confirm postal-to-zone mapping and pickup and delivery coverage. If a required country or mapping cannot be maintained in the available screen, ask implementation support to configure it before offering that lane.

### Configure and verify the route

8. Use **Default routes** to create the supported origin-to-destination route with the correct courier product, mode and transit hours. The current form creates a direct first leg; additional multi-leg arrangements require support where no editing control is available.
9. Use the service-area and temporary-closure controls available from routing configuration to record coverage, effective dates and operational closures. Specify pickup, delivery or both as appropriate.
10. Open **Route planner** and test the origin and destination postal codes with the intended product. Confirm that the route, facilities and serviceability result match the intended movement.

**A country in a dropdown is not proof of service availability.** International destinations also need valid postal data, coverage, zones, routing and prices. A postcode that passes format validation can still be unserviceable. Use route overrides only for an approved exception; ask support about reversal where the current screen has no removal action.

<!-- page -->
## Configure products and package presets

**Where:** Commercial → Courier products; Commercial → Pricing masters.

### Set up the courier product

1. Create or open the required courier product. Review its code, name, transport mode and transit or SLA settings.
2. Set the permitted weight range, weight rounding step, volumetric divisor and any dimension limits. Pay attention to units: configuration may ask for **grams**, while booking asks for **kilograms**.
3. Review the cutoff, effective period and status. Check the product's **COD** and **insurance** eligibility against the approved service offer.
4. Save and verify that the active product appears in **New booking**. Then confirm its route and price for a representative lane.

**Insurance eligibility alone does not create a premium.** An eligible product still needs a matching insurance rule in the applicable active rate-card version. Likewise, a saved product does not create postal coverage or transport rates.

### Set up reusable package types

5. In **Pricing masters**, create or review package presets with a code, name, dimensions, volumetric settings and maximum weight where supported.
6. Use clear names that operators can distinguish, such as an approved small carton or document pack. Check the saved preset in the booking form.
7. Train operators to measure the parcel and correct a preset when needed. A preset speeds entry; it does not certify the actual weight or contents.

### Understand chargeable weight

The preview compares actual and volumetric weight and applies the configured charging and rounding rules. A large lightweight carton can cost more than its scale weight suggests. Staff should read the chargeable weight and price explanation rather than manually replace the total.

### Use pricing masters for their intended role

State-wise base costs provide a fallback where a more specific configured lane or slab is not used. Do not assume that changing a state-wise base cost overrides an existing rate-card lane. Test with the actual customer and service to confirm which rule applies.

**Before publishing a service:** verify one normal parcel, a larger volumetric parcel, a boundary weight and an unsupported lane in an approved training setup. Confirm that the system accepts, prices or rejects each case as intended. Record the product and rate version used so staff know which configuration is effective.

<!-- page -->
## Publish a complete rate card version

**Where:** Commercial → Rate cards. Publication changes the prices used by applicable new quotes, so use the approved commercial schedule and effective dates.

1. Create or select the rate card. Review its code, name and **Scope**: **RETAIL**, **BUSINESS** or **FRANCHISE**. Check any customer or franchise assignment and the default retail choice.
2. Open the card and choose **Create draft version**. Enter the effective dates and a useful change note.
3. Populate every required lane using **Set lane rate**. Select the service and origin and destination zones, then enter the base weight, base price, additional step, additional price and minimum chargeable weight as applicable.
4. Use **Add surcharge** for each approved fuel, handling, insurance or other rule that belongs in the version. Review the basis, service restriction, limits, priority and tax treatment.
5. Review the entire draft against the approved schedule. When ready, select **Activate version** and read the publication confirmation before completing it.
6. Run the **Pricing simulator** and a booking preview using the intended customer, lane, product, package measurements and payment arrangement. Confirm the returned breakdown and effective version where shown.

**A new draft starts empty.** It does not automatically copy all lanes and surcharges from the previous version. Re-enter the complete intended configuration before activation. Adding only a new insurance rule can leave the new version without required transport pricing.

### Read the units correctly

| Input | How to enter it |
| --- | --- |
| Base weight and additional step | Grams when the configuration label says g |
| Price and fixed charge | Normal currency amounts as displayed, such as 1500.00 |
| Percentage | Enter 2.5 for 2.5 percent, not 0.025 or 250 |
| Origin and destination zone | Configured zone code, not a town name or postal code |
| Service code | The existing courier product code |

**After activation:** the version is immutable. Correct a published schedule with a new complete draft and an approved effective date. Historical bookings retain their saved commercial information; changing a future rate is not a way to rewrite an old charge.

**If no price is found:** check the card assignment, active dates, product and zone codes, lane direction and weight rules. A route can be serviceable while its applicable price is missing. Resolve the configuration instead of substituting an unrelated customer's rate.

<!-- page -->
## Configure shipment insurance

**Where:** Commercial → Rate cards → selected card → draft version → **Add surcharge**. First confirm that the courier product permits insurance.

1. Enter a clear **Code** and **Name**. Set **Type** to **INSURANCE**.
2. For a percentage of goods value, set **Calculation** to **PERCENTAGE**, enter the approved **Percentage**, and set **Applies to** to **DECLARED_VALUE**.
3. Enter an optional minimum and maximum charge in the card's currency. Restrict **Service code** if needed. Set priority and **Taxable** according to the approved schedule.
4. Select **Add surcharge**. Complete the remaining rates and rules, review the draft and activate the version as described on page 6.

![Insurance surcharge configuration with an illustrative percentage and charge limits](../assets/insurance-configuration.png){width=3.85}
*Illustration only. The 2.5% rate and limits are not prescribed settings.*

**Rules can add together.** A service-specific rule does not automatically replace a matching unrestricted rule. Check for unintended overlaps. Fixed and per-kilogram insurance calculations are also supported; the customer's acceptance may show a premium amount rather than a simple percentage.

**Verify:** preview the same shipment without insurance and then with insurance and a positive declared value. Check the premium, any limits and taxes, and the final total. Booking insurance also requires the customer's acceptance of the current quote. If no eligible rule exists, the application reports that insurance is not configured; it does not fall back to 1%.

<!-- page -->
## Prepare customer accounts and release the setup

### Commercial accounts and finance arrangements

1. Use **Commercial → Customers → Add customer** for retail and business records. Business accounts need an **Account code** and **Legal name** in addition to the contact details.
2. Set the approved credit limit, payment terms, billing cycle and rate-card assignment where applicable. Review the customer's **Billing** tab and authorized credit-term controls.
3. Confirm the account is active before booking. Test **CREDIT** only with an eligible billing account. Third-party payers must be active authorized accounts in the same organization.
4. Ask finance to review tax treatment, commission, ledger and settlement configuration before using those financial workflows. Where the application has no configuration control, use implementation support; do not alter booking totals to simulate a missing rule.

### Check readiness with the people who will use it

- **Counter staff:** can create or select an active customer, enter addresses, preview a price, capture insurance acceptance and see the resulting AWB in an approved practice booking.
- **Operations:** can access the correct facility, identify IDs requested by the forms, scan with an Enter suffix, and follow the bag, manifest, trip and receiving workflow.
- **Dispatch:** can assign an authorized pickup or delivery agent and see the appropriate run and stop actions.
- **Finance:** can distinguish prepaid customer collections from COD, review billing parties and amounts, and identify who is allowed to approve or accept each financial action.
- **Support:** knows the approved escalation contact, live application address and exact details to collect when a user is blocked.

### Review the configuration regularly

Review staff access when roles change, effective dates before rate changes, unresolved coverage errors, account status and credit restrictions, and operational closures. After a change, repeat the relevant preview or route check and brief the affected staff.

Customer notifications require configured providers and delivery settings. A saved template or enabled trigger alone is not proof that a real SMS, WhatsApp message or email was delivered. Verify the relevant delivery record and provider setup with support.

**Current boundaries:** customer login linking may require administrator support outside the visible form; invoice PDF rendering is unavailable in the current billing screen; customs details do not perform duty assessment or electronic filing; saved-address and bulk geography forms have domestic-oriented limits. Escalate these needs rather than describing them to users as completed features.

<!-- page -->
## Configure notification templates and triggers

**Where:** Administration → Notification templates and Notification triggers. Templates control message wording; shipment events control when a notification is considered for delivery.

1. Review existing templates by event and channel before creating another. Identify the intended audience, milestone, language and approved wording.
2. Choose the create action and supply **Code**, **Name**, **Event trigger**, **Channel** and **Locale**. Channels are SMS, EMAIL, WHATSAPP and PUSH; selecting one does not configure its delivery provider. Use the approved locale, for example **en-NG**.
3. Enter the **Provider reference** when the channel requires a registered sender or approved provider template. For EMAIL, also supply **Subject**.
4. Write the **Body** using supported variables in double braces, such as **{{awb}}**. The application derives the referenced variables and validates the template.
5. Save, then use **Preview** with fictional sample values. Read the rendered text and resolve every missing-variable message. A successful preview renders content; it does not send a real message.
6. Review the template's active state. Use the edit action for supported changes. Event, channel and locale identify the template context and are not freely changed while editing an existing record.

### Check milestone coverage

Open **Notification triggers** to see which events have template coverage. The current event choices include shipment booked, pickup completed, in transit, out for delivery, delivered, NDR and RTO.

A milestone can send only when the required active template, channel and locale are available for the recipient. The event screen shows coverage; it is not a custom automation designer. Users cannot create arbitrary shipment state transitions by adding a template.

### Maintain approved wording

Keep messages concise and accurate about the recorded event. Avoid promising delivery dates, refunds or insurance benefits that the system has not established. Check the contact details used by the relevant shipment or account when investigating missing messages.

Template version history and rollback are not offered in this interface. Retain the approved text and effective change record through your organization's process before replacing it. Recheck the preview after every change, and verify delivery separately using the channel and delivery screens on the next page.

<!-- page -->
## Monitor notification delivery and channel health

**Where:** Administration → Notification channels and Notification deliveries. Use these screens to distinguish a composed message from a real provider delivery attempt.

1. Open **Notification channels**. Review each channel's configured state and the last 24 hours of delivery outcomes.
2. Ask the deployment administrator to confirm the active provider if a channel is unavailable or only logging messages. Channel adapters and provider credentials are deployment settings, not editable channel records.
3. In **Notification deliveries**, filter by status, channel or event. Open the relevant message and verify the recipient, event, rendered message, state and timestamps.
4. Inspect **Provider attempts**, response details, latency, error and retryability. Use this evidence when deciding whether the recipient was contacted.
5. After the cause is fixed, use **Retry** when offered for FAILED or DEAD_LETTER records. This resets the attempt budget and queues another attempt; it may contact the recipient again.
6. Use **Cancel unsent message** only when authorized and offered for PENDING or SENDING records. Verify the resulting state; a cancellation request is not evidence that an already delivered message was recalled.

### Understand provider readiness

The implementation includes Termii adapters for SMS and WhatsApp when configured. Other channel choices, or a logging fallback, do not establish that a live provider is sending. Confirm the actual deployment with the technical owner and verify a controlled end-to-end message before relying on customer notifications.

**Configured** means the application reports a sender is available. A rendered preview or internal success log alone is not proof the intended person's device received the message. Review the real provider evidence and any delivery receipt available to your operations team.

### Investigate without creating duplicate messages

For a missing notification, check whether the shipment milestone actually occurred, whether an active matching template exists, whether the channel is ready, and what the delivery record says. A suppressed message may not have any provider attempt. Fix the cause before requesting a retry.

Give support the notification reference, AWB or event, channel, time, state and exact error. Avoid copying full customer contact information when it is not needed. Do not use repeated delivery retries as a connectivity test against live customers.

<!-- page -->
## Issue and maintain partner API credentials

**Where:** Administration → API credentials. An API credential lets an approved external system perform selected partner operations. It is different from a staff password or a customer portal login.

1. Confirm the partner, responsible owner, intended functions, permitted customer mapping and approved environment with implementation support.
2. Select **Issue API credential** and enter a meaningful name. Select only the required scopes. A key with no scopes cannot call partner operations.
3. Add approved **Allowed CIDRs** if the connection is restricted to known network ranges. Set an expiry and requests-per-minute limit where required by the integration plan.
4. Issue the credential and store the one-time **Authorization token** immediately in the partner's approved secure credential store. Confirm successful storage before closing the dialog.
5. Verify a controlled call with the partner, then open the key's usage view. Check recent routes, outcomes and errors and confirm that only the approved work is occurring.

### Choose scopes by the agreed job

| Scope family | Capability |
| --- | --- |
| serviceability and pricing read | Check coverage and obtain a quote |
| shipment create read and cancel | Book, inspect or cancel eligible shipments |
| tracking read and label read | Retrieve journey updates and labels |
| pickup create and read | Request and inspect collections |
| pod read | Retrieve permitted delivery evidence |
| webhook manage | Manage the connection's event delivery setup |

Use the exact scope labels displayed in the form. These scopes are access grants; the partner still needs valid configuration, record scope and state eligibility. The visible issuance form is not a merchant-to-customer mapping wizard.

### Suspend replace or revoke

Use **Suspend** with a reason to pause a key, then **Resume** only after the issue is resolved. **Revoke** permanently disables it; it cannot be recovered or reactivated. For planned replacement, issue and verify the new key with the partner before revoking the old one.

If the token is lost, issue a replacement through the authorized process. Never put tokens in spreadsheets, screenshots, support tickets or source code. Usage records provide operational metadata without exposing the secret. A partner receiving access errors should check expiry, status, scope and allowed network before requesting broader access.

<!-- page -->
## Configure webhooks and inspect partner delivery

**Where:** Administration → Webhook endpoints and Webhook deliveries. A webhook sends a business event to an approved external system after the underlying business transaction has committed.

1. Confirm the partner's public HTTPS receiving URL and the events it needs. Obtain an owner who can verify messages and investigate failures on the receiving side.
2. Choose **Register webhook endpoint**. Enter its name and HTTPS URL, select the supported events and save. Registration does not contact the URL or prove it works.
3. Store the one-time **Signing secret** securely against this endpoint. The receiving system uses it to validate messages; it is different from the API token.
4. Review the endpoint's event subscriptions. Enable only agreed events and verify them with a controlled transaction. The application publishes customer-safe business events rather than every internal handling scan.
5. Open **Webhook deliveries**, select the delivery, and inspect the endpoint, event identity, payload, state and attempt history. Compare the HTTP response and errors with the partner's receiving log.
6. Correct the receiving issue before using **Replay delivery** when permitted. Replay creates a new delivery while preserving the original event identity and original attempt evidence.

### Operate the connection reliably

Delivery can occur more than once. The receiving system must recognize a repeated event and avoid making a duplicate order update, refund or other business action. Ask the integration owner to verify signature checking and event deduplication before enabling the connection.

Use **Pause endpoint** for a temporary interruption and **Resume endpoint** after correction. Resume clears the consecutive-failure counter. **Disable endpoint** stops delivery to that endpoint and retains its history. Repeated failures can also pause delivery; review the error instead of repeatedly resuming a broken receiver.

### Important configuration limits

URLs must meet the partner service's public HTTPS requirements. Private or local network addresses are not appropriate receiving endpoints. Ask the partner for the production callback URL rather than using a staff browser URL.

Subscription filters are stored but are not currently enforced. Do not rely on such a filter to isolate a customer or hide events. Verify the actual authorized connection scope with implementation support. To rotate the signing secret, coordinate a replacement endpoint and a controlled cutover; do not assume a secret can be retrieved later.

<!-- page -->
## Onboard an ecommerce merchant connection

**Audience:** the administrator and the merchant's integration owner. The partner API supports store-to-courier workflows, but the staff interface has no complete store-connection wizard. The native Ahiabekee integration contract is documented; other store platforms require an implemented adapter, not merely an API key.

### Agree the business mapping

1. Create or select the merchant's active logistics customer under **Customers**. Record its actual customer ID for the integration owner. The shopper is normally the recipient, not a new logistics merchant account for every order.
2. Agree the active courier product, supported lanes, parcel measurements, COD policy and declared-value rules. Confirm coverage and prices with a simulator or approved practice booking.
3. Confirm organization country, currency and timezone, and the sender warehouse address snapshot. Both sender and recipient must carry the correct country and postal information.
4. Ask implementation support to provision and verify the store-to-customer mapping and partner authorization. Do not infer isolation from the API key's display name.

### Verify the connection before releasing orders

5. Issue the approved minimum credential scopes and store the token securely. Booking and synchronization commonly need shipment create, shipment read, tracking read and webhook manage; add label, POD or pickup scopes only if used.
6. Register the signed webhook receiver, store its secret and agree which business events the merchant will consume.
7. In the approved test environment, send a prepaid order and verify the one resulting AWB, customer mapping, service, addresses, package values and price. Repeat with COD and verify the amount to collect.
8. Repeat the same request through the adapter's retry process. Confirm it resolves to the original booking rather than producing a second shipment. Verify duplicate webhook handling and tracking recovery after a missed event.
9. Enable live order handoff only after both teams agree that booking, status synchronization and failure handling are correct. Retain the connection owner and supported escalation route.

### Handle changes and shutdown

Coordinate credential replacement and endpoint changes with the merchant. Stopping new orders is different from stopping status updates for shipments already travelling. Keep mappings and valid event processing for those shipments until the agreed cutover is complete.

Technical implementers should use the repository's Ecommerce integration and Merchant onboarding contracts for request formats, signatures and retry behavior. This user manual describes the administrative handoff, not a claim that Shopify, WooCommerce or every other store platform has a ready-to-install connector.

<!-- page -->
## Verify readiness and maintain the whole configuration

Complete these checks with the responsible business owners. Use an approved training environment for practice records and financial actions. A successful save on a configuration screen is only the first check.

| Owner | Evidence to review before release |
| --- | --- |
| Administrator | Correct organization; individual accounts; permitted menus and actions; correct facility and portal links |
| Network manager | Valid postal data and zones; pickup and delivery coverage; route result and any active closures |
| Commercial manager | Product limits; complete active rate version; representative normal and volumetric quotes; correct insurance premium |
| Counter supervisor | Customer creation and result selection; addresses; customs and payer instructions; AWB and usable package labels |
| Operations manager | Assigned pickup; scanner acceptance; bag and manifest closure; trip receipt and discrepancy reconciliation |
| Delivery manager | Eligible run dispatch; actual attempt; COD amount; POD; NDR and approved return workflow |
| Finance manager | Collections; matched commission rules; ledger balance; COD custody; invoice and settlement review and approval |
| Support and integration owner | Portal scope; real notification evidence; signed partner events; usable report exports and named escalation contacts |

### Review changes before they take effect

Check rate and commission effective dates, customer credit arrangements, product eligibility and operational closures regularly. Communicate the changed service, date and expected behavior to affected teams. New rates do not rewrite saved shipment snapshots.

Before changing staff scope, reassign active work and money responsibilities. Before changing a provider or partner endpoint, agree the cutover and verify an actual delivery. Before changing a finance rule, run a simulation and review a representative posted result with finance.

### Keep an approved reference record

Maintain the live application addresses, support owners, operating-unit IDs needed by forms, product and zone codes, approved commercial schedules, provider and partner owners, and current capability limitations. Store secret credentials separately in the approved credential system.

**When setup needs support:** missing portal account linking, unsupported geography mappings, additional route editing, provider deployment, merchant scope setup, financial correction queues and invoice PDF generation cannot be solved by typing a different value into an unrelated field. Record the required capability and affected workflow and refer it to implementation support.
