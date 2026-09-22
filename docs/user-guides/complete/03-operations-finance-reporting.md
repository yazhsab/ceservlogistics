# CESERV Operations Finance and Reporting

For pickup warehouse dispatch delivery support finance and management teams

This manual follows the daily courier journey and the money records it creates. It covers collection, scanning, bags, manifests, fleet, trips, receiving, delivery, failed attempts, returns and proof, then explains collections, COD, commission, ledger, settlements, billing and management reporting.

**Edition:** 18 September 2026. Use your own staff login at https://app.ceservlogistics.com/login. Work only within your assigned scope and record actual physical or financial events. Your manager defines local cutoffs, cash procedures, approvals and escalation contacts.

### Begin the shift

Check your access and facility, review assigned work and the previous handover, clear stale queue filters, and verify the scanner and printer. Confirm who owns unresolved parcels, missing scans, count differences and cash discrepancies before taking over.

| Task | Page |
| --- | --- |
| Booking handoff and pickup runs | 2 |
| Scanner operation | 3 |
| Bags and manifests | 4 |
| Fleet vehicles carriers and drivers | 5 |
| Trips receiving and reconciliation | 6 |
| Destination dispatch and delivery | 7 |
| NDR returns and proof of delivery | 8 to 9 |
| Customer collections and COD custody | 10 to 11 |
| Commission rules simulation and entries | 12 to 13 |
| Ledger journals and trial balance | 14 |
| Franchise settlements and customer billing | 15 to 16 |
| Report catalogue and command centre | 17 to 18 |
| Shift handover and operational checks | 19 |

**Reference discipline:** AWBs, customer codes, container codes and record IDs identify different things. Use an actual ID when a field explicitly requests one. The Complete Product Walkthrough contains the detailed customer and booking instructions.

<!-- page -->
## Book parcels and arrange pickups

### Counter checklist

1. Search for the customer. If no active account exists, create one under **Commercial → Customers** when authorized. In **New booking**, click the customer search result.
2. Confirm sender and recipient addresses, country and postal code. Record the real package weight and dimensions, contents and declared value.
3. Agree the service, payment mode, transportation payer, duty payer and insurance decision. Add customs goods lines when required.
4. Preview, review the charge breakdown, obtain acceptance of the current insurance premium if requested, and select **Confirm & book** once.
5. Check the AWB and label. Keep payments separate from booking confirmation: a successful booking does not itself prove money was received.

The Complete Product Walkthrough provides the detailed booking walkthrough. If a save response is uncertain, check **Shipments** before creating a second booking.

### Dispatcher pickup workflow

6. Open **Operations → Pickups** and use **Raise pickup** when a collection is needed. Identify the customer and collection address, contact, requested time window, expected pieces and linked shipments where known.
7. In **Pickup runs**, create a run with the correct branch, pickup date, agent and vehicle reference where applicable.
8. Assign the selected pickups to the intended agent and run. Some controls request customer, agent, address, run or shipment IDs; use the actual IDs from your approved reference records.
9. Confirm the assignments appear in the queue and that the agent can see the pickup in **My pickup route**.

### Pickup agent workflow

10. Open **My pickup route** and the assigned stop. Check the contact, address, time window, expected pieces and instructions before attending.
11. At the stop, verify the parcels and record the visit outcome, actual pieces and weight and the collected shipment references requested by the form.
12. If collection fails, select the correct failure reason, record useful remarks and the next attempt when appropriate. Save and confirm the updated status.

**Do not mark completion in advance.** If the pickup is incomplete or the parcel differs from the booking, record the actual outcome and contact dispatch. Follow your organization's process for updating charges or booking details; do not silently substitute another shipment's label.

<!-- page -->
## Use the scanner console

**Where:** Operations → Scanner. A scanner generally enters text like a keyboard. The application needs the correct operating context before it can accept a scan.

1. Select or enter the **Current facility** and the intended operation. Confirm the facility ID with your supervisor if the screen requests one.
2. Focus the scan input. Configure the scanner to send an **Enter** suffix after the barcode. For manual entry, type the barcode or AWB and press Enter.
3. Scan one item and read the accepted or rejected result. Check the recent scan list before repeating a scan that appears slow.
4. On a rejection, read the stated reason, correct the facility, operation or parcel context as appropriate, and then retry. Keep unresolved parcels separate for review.
5. Continue only after the console is ready for the next scan. Recheck the mode whenever you change tasks.

| Mode | Use |
| --- | --- |
| RECEIVE | Record receipt at the current facility when the parcel is eligible |
| ARRIVAL or DEPARTURE | Record the corresponding movement event in the permitted context |
| SORT | Record sorting or routing information; this alone does not transfer custody |
| HOLD | Record a hold with the required reason |
| RELEASE | Clear an eligible hold through the authorized action |
| DAMAGE | Record a damage event and its reason |
| EXCEPTION | Record an operational exception for follow-up |

### Keyboard and hardware tips

Use **Tab** to move between fields and the mode shortcuts shown by the console, including **Alt plus the displayed number**. Keep focus in the scan field during repetitive scanning. If the barcode appears but nothing submits, press **Enter** and check the scanner suffix setting.

The application has no scanner connection wizard. Test whether the device types correctly in an ordinary text editor before using a live operations screen. Scanners without a suffix may not submit reliably. A device beep confirms it read a barcode; the application's accepted result confirms the operational action.

**If the internet drops:** stop and reconcile recent results when the connection returns. Do not assume offline scans were saved. **If a scan is rejected repeatedly:** send the supervisor the AWB or barcode, facility, selected mode, time and exact error. Do not try unrelated modes merely to force an accepted result.

<!-- page -->
## Build bags and manifests

### Create and close a bag

1. Open **Operations → Bags → Create bag**. Confirm origin, destination, bag type and direction, and any product, piece or weight limit.
2. Open the bag and scan eligible shipments into it. Check each result and the **Current contents** list against the physical parcels.
3. Resolve missing, duplicated, wrong-destination or held parcels before closing. Check the piece count and weight.
4. Select the close action. In **Review and close bag**, enter the actual seal number or numbers, seal type and gross weight requested by the form. Read the summary, then select **Close bag**.
5. Verify the closed state and declaration. Use the available label action and attach the correct bag label and seals.

**Closed contents are fixed.** Do not break a seal or edit the contents without the authorized exception procedure. If the contents are wrong after closure, stop the bag and ask the supervisor to use the permitted correction workflow.

### Create and close a manifest

6. Open **Operations → Manifests → Create manifest**. Set the correct origin, destination, direction and trip reference if applicable.
7. Add eligible closed bags and any permitted loose shipments. Do not add a shipment again as a loose item when it is already inside an included bag.
8. Review all references, destinations, piece counts and weights against what will travel. Use **Review and close manifest**, then **Close manifest** when correct.
9. Check the closed state. If a print document is being prepared, wait for it to become ready before downloading or printing. Follow the next permitted dispatch or trip action.

### Receive the physical contents

At the destination, use **Receive bag** or **Receive manifest** in the correct facility context. Match the arriving identifier and paperwork. Verify the bag's seal using the actual seal number and observed result. Record damage or mismatch rather than confirming an intact seal by default.

Open and reconcile contents only through the actions offered for the current state. A container receipt and a complete item count are separate checks. The Hub operations instructions on page 6 explain reconciliation.

**If an action is missing:** review the container status, destination, your facility and your permission. A closed declaration cannot be treated as an editable draft simply because a parcel was forgotten.

<!-- page -->
## Maintain fleet vehicles carriers and drivers

**Where:** Operations → Trips → Fleet and carriers. These low-volume records supply the assignments used by trip planning. Prepare them before a dispatcher needs to depart a road trip.

### Create the planning resources

1. Open **Carriers** and add the approved organization or transport provider. Enter its code and name, choose OWN, CONTRACTED, PARTNER or COURIER_PARTNER as appropriate, and record supported modes and contact details.
2. Open **Vehicles** and add the registration number, vehicle type, carrier ID where applicable and capacity. Capacity is entered in **grams**; confirm the conversion from the approved vehicle capacity.
3. Open **Drivers** and enter the code, full name, phone, licence details and carrier ID where applicable. Check that the right person is represented before assignment.
4. Review the saved records and their status. In a practice trip, confirm that the appropriate vehicle, driver and carrier can be selected.

### Use the resources on a trip

5. Create or open the trip and choose **Assign vehicle and crew** when needed. Select the actual carrier, vehicle and driver; enter the external movement reference for modes that require one.
6. Save and review the vehicle and crew summary. Do not infer that saving an assignment departs the trip.
7. Before departure, check the physical registration and driver against the selected records and check that the loaded movement is appropriate for that resource.

### Resolve assignment failures

The server checks live-trip conflicts. If a driver or vehicle is rejected because it is already in use, inspect the earlier trip and determine whether it actually arrived or needs an authorized correction. Do not create a duplicate driver or vehicle record to bypass the conflict.

For air, rail or partner movements, use the real flight, train or partner reference required by the trip workflow. A carrier record alone does not establish a carrier integration or automatically import its tracking.

**Available scope:** this workspace creates planning resources and shows their records. It is not a vehicle maintenance, fuel, GPS telematics, licence-renewal or payroll system. Refer changes or lifecycle actions not offered by the screen to the authorized administrator.

**Successful setup:** dispatchers can identify the correct resources, assign them to eligible trips and satisfy departure requirements without guessing IDs or substituting another resource.

<!-- page -->
## Move trips and reconcile inbound parcels

### Outbound dispatcher

1. Open **Operations → Trips** and create the trip with the correct mode, origin, destination and scheduled times.
2. Assign the vehicle and driver for a road movement. For other modes, provide the appropriate flight, train or partner reference required by the screen.
3. Attach the eligible closed manifests. Compare the loading list with the physical bags and loose parcels.
4. Use the departure action only when loading and the required assignments are complete. Confirm that the trip status changed.
5. At destination, record the trip's arrival through the available action. Then complete facility receiving; arrival alone does not receive the contents of every manifest or bag.

### Hub or branch receiving team

6. Open **Operations → Hub operations** in the correct facility. Review expected inbound trips and manifests and match the arriving movement.
7. Receive the eligible manifest and bags, checking identifiers and seals. Use the appropriate receiving actions rather than only updating the trip.
8. Open the reconciliation for the arriving container. Scan the actual contents and compare the expected and received counts.
9. Identify missing, excess, damaged or misrouted items. Record the applicable condition and remarks. Do not scan an absent parcel just to make the count agree.
10. Complete reconciliation after the physical count and discrepancy review. Follow the resulting exception queue for unresolved differences.

### Resolve exceptions and prepare outbound work

Review each exception with its AWB, source container, expected and actual facility, and scan history. Select the supported resolution action and provide meaningful notes. Closing an exception without explaining the correction makes the next shift's work harder.

Sort eligible parcels to their next destination using the system's routing result. Create the next bag or manifest only after required receipt and custody steps are complete. A parcel on hold or in an incompatible state may be correctly rejected from outbound handling.

**What success looks like:** trip and container statuses reflect the real movement; physical and recorded counts agree, or the differences have recorded exceptions; outbound parcels are in the correct custody and eligible state.

**Escalate with evidence:** include the trip, manifest and bag references, affected AWBs, seal result, observed counts, facility and time. Retain the actual parcel and supporting paperwork according to the supervisor's instructions.

<!-- page -->
## Dispatch and complete deliveries

### Destination dispatcher

1. Open **Operations → Destination** to see the **Destination branch queue** and choose the correct branch. Review the parcels ready for delivery and any held or exception items.
2. In **Operations → Delivery**, select **Create run**. Enter the branch, agent, date, vehicle reference where applicable and initial AWBs if ready.
3. Add eligible shipments from the destination queue or use **Add stops** with their AWBs or barcodes. Check the agent's workload and delivery area.
4. Review the run, then select **Dispatch run** when the agent is actually leaving with the parcels. Dispatch is the action that makes shipments **OFD**, meaning out for delivery.

A shipment must be in the appropriate branch custody and eligible state. It cannot belong to two active delivery runs. Resolve the prior run or custody issue instead of creating a duplicate stop.

### Delivery agent

5. Open **My delivery run** and the assigned stop. Review the recipient contact, address, instructions and expected COD before attending.
6. At the stop, choose the actual **Outcome** and confirm the current branch context. For delivery, record the recipient name and relationship and complete the required verification.
7. If the workflow requires an **OTP**, use the authorized issue and verification process. A newly issued code replaces the previous one. Do not save codes in notes or share them outside that verification process.
8. For COD, record the exact amount actually collected using the displayed expected amount, payment method and required reference. If the full required amount is not collected, record the appropriate unsuccessful outcome and contact dispatch.
9. Submit the outcome once, wait for confirmation and check the stop status. Add proof of delivery through the permitted POD action, including the evidence required by your organization.

### When delivery cannot be completed

Select the correct failure reason, record concise useful remarks and a next-attempt time when appropriate. Follow the NDR case rather than marking the parcel delivered to remove it from the queue.

**Verification and evidence are separate records.** Do not assume a photo automatically records a delivery or cash receipt. Check the resulting stop, shipment and COD statuses. Notification delivery depends on configured providers; seeing an OTP issue response alone does not prove the customer received a message.

**No active run:** ask the dispatcher to check the run date, agent assignment, dispatch status and your login. Do not use another agent's account.

<!-- page -->
## Handle failed delivery and returns

### Work an NDR case

**NDR** means non-delivery report. It records a failed delivery and the next action needed to resolve it.

1. Open **Operations → NDR** and find the case by the available queue or shipment reference.
2. Read the failed attempt, reason, contact history and previous actions before calling the customer or arranging another attempt.
3. Confirm the correct address, recipient availability and any collection or return instruction through the approved service process.
4. Set the appropriate next action offered by the screen: reattempt, reschedule, contact required, address correction, customer pickup, return to origin or escalation.
5. Enter the requested date, correction and notes. Save and check the updated case. A new next action replaces the earlier plan, so tell the responsible dispatcher when the plan changes.

An address correction recorded in the NDR case does not silently rewrite the original booking address. Make sure the delivery team has the current instruction and uses the supported follow-up workflow.

### Move an approved return

**RTO** means return to origin. Use it when return is authorized, rather than to clear an unresolved delivery queue.

6. Open **Operations → RTO**. Initiate the return for the correct AWB or barcode with the decision notes requested by the form.
7. Follow the reverse route and normal physical handling controls. Record receipt at each required facility; a return still needs accountable custody.
8. At the origin, verify the parcel and arrange handover to the sender. Use **Complete sender handover** only after the actual attempt, recording its outcome, recipient and remarks as requested.
9. Check the final return status. If the sender handover failed, keep the return open in the appropriate state and escalate; the parcel has not been returned merely because it reached the origin branch.

### Proof and customer communication

Use **POD** to view or submit supported proof of delivery with the proper permission. Check the AWB before uploading a signature, photo or document. Saved evidence is an accountable record; mistakes need the approved corrective process rather than an undocumented replacement.

Give customers the current factual status and agreed next step. Avoid promising a delivery or refund that has not been approved. In a dispute, gather the AWB, attempt history, instructions and relevant evidence for the authorized supervisor.

<!-- page -->
## Capture and review proof of delivery

**Where:** Operations → POD, or an authorized link to an existing POD record. Proof is evidence associated with a shipment; it does not replace the separate delivery-attempt and payment records.

### Submit accurate evidence

1. Confirm the AWB or barcode against the physical parcel and delivery record before opening the evidence form.
2. Select the appropriate **POD type** for the event, such as delivery, return handover, customer pickup or handover, using the choices offered by the form.
3. Enter the recipient name and relationship, and the current facility and other requested details. Enter phone or identification details only when the authorized procedure calls for them.
4. Attach the required signature image, delivery photo or document. Check that the file depicts the correct parcel or event and is readable. Follow the permitted file types and sizes shown by the upload control.
5. Enter useful remarks and review the references and evidence before submitting once. Files are checked by their actual content, so renaming an unsupported file does not make it valid.
6. Confirm the saved POD record and verify that the associated shipment and delivery outcome are correct.

### Inspect a saved POD

Open the permitted POD detail. Check the AWB, recipient, delivery time, facility, verification and remarks. Load evidence artifacts only when needed for the task. Access may depend on the user's scope and is recorded by the service.

An empty artifacts area can indicate restricted access or unavailable evidence. Do not conclude that no delivery occurred from that area alone; review the recorded attempt and request the necessary authorized access.

### Correct an evidence problem

POD evidence is append-only. Do not expect an ordinary edit or silent replacement of the original. If an incorrect file or recipient was submitted, retain the POD reference, explain the error and ask the supervisor to use the approved correction process.

For delivery disputes, correlate the evidence with the actual attempt, OTP verification where used, contact history and any COD receipt. A photograph alone is not proof that the full COD was received, and a collected payment alone is not a signed delivery record.

**Customer requests:** the current customer portal does not provide customer-scoped POD retrieval. The authorized service team should handle the request through the organization's approved sharing process. Do not give the customer staff access to retrieve a document.

<!-- page -->
## Record customer collections correctly

**Where:** Finance → Customer collections. The page is **Franchise customer collections** and records eligible prepaid cash, POS and transfer receipts at the origin counter. It is separate from destination COD.

### Record the actual full shipment payment

1. Confirm that the shipment was booked under the correct customer, payment arrangement and origin franchise. Open the saved shipment and verify its authoritative total.
2. Enter its **Shipment ID**, not its AWB, in the collection form. Obtain the actual record ID where no lookup is provided.
3. Enter **Amount collected** in the displayed currency. This form requires the full shipment total, so do not use it for an arbitrary partial payment.
4. Select the actual method: CASH, POS, TRANSFER or BANK_DEPOSIT. Supply a **Transaction reference** for non-cash payments and any required reference for the organization's procedure.
5. Submit once and wait for confirmation. Review the **Collection register** for the saved amount, collector and settlement relationship. Compare the reference against the real receipt or bank evidence.

### Choose the right financial record

| Record | What it establishes |
| --- | --- |
| Shipment preview | A quote; no booking or payment has occurred |
| Saved booking | A shipment and price snapshot exist; cash is not automatically recorded |
| Customer collection | An eligible origin prepaid shipment receipt was recorded |
| COD obligation and custody | Money collected from a recipient and who is responsible for it |
| Invoice payment | A real payment was applied to an issued customer's invoice |
| Settlement payment | A real payment was applied to a franchise statement |

### Investigate before repeating a receipt

If a request times out, inspect the register and saved shipment before submitting again. If the amount is rejected, check the correct shipment, payment mode, full total and franchise scope. A different amount may belong to another financial workflow or need finance review.

Do not record the same cash in customer collections and COD merely to make both screens look complete. At shift end, reconcile the physical cash and non-cash references with the relevant register, record any difference and arrange the approved custody handover.

**Escalate with:** shipment ID and AWB, collection reference, method, amount, collector, date and the exact error. Booking confirmation alone is not a receipt.

<!-- page -->
## Reconcile COD and follow cash custody

**Where:** Finance → COD control. COD means the amount to collect from the recipient; it may differ from both goods value and transport charges.

1. Find the obligation by AWB. Read the expected, collected, adjusted and remitted amounts, current holder and custody timeline.
2. Compare the obligation with the delivery outcome and actual collection. In the current operational flow, a COD obligation is opened when a qualifying delivery is completed; a booking marked COD alone does not create a cash-in-hand liability.
3. Use **Check custody** for the relevant party. Confirm the party type and actual record ID rather than substituting its name or facility code.
4. When authorized, open **Reconcile custody**. Choose the party and period, identify the obligations, count the actual amounts and record the requested notes and variance reason.
5. Save and inspect the result. Investigate shortages and excesses using the delivery and handover evidence instead of changing the expected amount to match a count.

### Separate reconciliation from handover

A reconciliation compares records and actual amounts. A custody transfer records money passed between accountable parties. A remittance sends money onward under the approved process. These are separate financial events.

The current holder remains accountable until the receiving party accepts the handover through the supported workflow. Physical cash movement without a confirmed recorded transfer can leave the former holder liable. Arrange the real handover and the required acceptance together.

### Current screen boundaries

The visible COD workspace provides the obligation register and detail, summary, custody check and reconciliation actions. It does not provide complete auditable queues or screens for pending transfers, remittances, adjustments and disputes. Those backend capabilities require authorized finance or implementation support where no screen is offered.

Do not tell an agent that a reconciliation or a button click transferred money unless the transfer and acceptance records show it. Preserve the relevant obligation IDs, amounts, sender, receiver, handover time and variance explanation for the finance team.

### Resolve failed delivery collection

A successful COD delivery requires the expected full collection in the delivery workflow. If money was not collected, record the actual unsuccessful outcome and contact dispatch. If an operational completion fails because its financial recording failed, retain the error and verify the current state before retrying; do not separately create compensating money records without investigation.

<!-- page -->
## Configure and simulate commission rules

**Where:** Finance → Commission. Use rules to define who earns commission and for which qualifying work. Finance approves the commercial policy; the application chooses and calculates the applicable rule.

1. Review the rule register using commission type and status filters. Open an existing rule to inspect scope and version history before creating an overlapping rule.
2. Select **Create rule**. Enter the existing **Scheme code**, rule code and name, commission type and recipient role. Add franchise, franchise category, service, customer category or payment scope where required, plus priority and description.
3. Save, open the rule and select **New version**. A rule without a rate version cannot calculate commission. Choose FIXED, PERCENTAGE or SLAB and the effective date.
4. Enter the relevant fixed amount, percentage rate and basis, or complete the slabs. Add approved minimum and maximum amounts and notes. Review units carefully before saving.
5. Open **Simulator**. Supply the commission type, franchise ID where relevant and the qualifying freight, surcharge, total, COD, chargeable weight and shipment count. Run the simulation.
6. Review the selected rule, version, candidates and calculation trace. Confirm that the intended rule wins and the returned amount matches the approved policy before relying on it operationally.

### Commission units differ from insurance entry

| Input | Meaning |
| --- | --- |
| Fixed minimum and maximum amounts | Normal currency values when the field shows ₦ |
| Rate in basis points | 100 basis points means 1%; enter 250 for 2.5% |
| Slab From units and To units | Whole units of the selected basis; confirm the basis with finance |
| Commission minor units in slabs | Kobo for NGN; 100 minor units means ₦1 |
| Chargeable weight | Grams in the simulator |

Slabs must start at zero and be contiguous, with an open-ended final slab as required by the form. Monetary bases use their underlying minor units; weight and count bases use their own units. Ask finance to verify a boundary example.

**Limits:** scheme codes must refer to configured schemes; there is no editable scheme register. The screen can name the winning rule/version without a direct detail link. A simulator result does not create a payable entry. Used versions preserve historical calculations; make approved changes through a new dated version.

<!-- page -->
## Review post and reverse commission entries

**Where:** Finance → Commission → Entries. The register shows saved calculations and their state: CALCULATED, POSTED, REVERSED or CANCELLED.

### Understand where entries come from

The current product connects configured commission rules to qualifying booking, origin handling, transit handling, destination handling and delivery transitions. Franchise relationships and operating-unit custody determine the recipient. These matched operational calculations are posted within the operational transaction using the shipment's saved charge snapshot.

No matching rule or eligible recipient means no commission is earned for that event. The rule-type list contains additional categories, but selecting a category alone does not establish that every operational event automatically triggers it. Verify the intended earning event and recipient with finance and implementation support.

### Review the entry before a financial action

1. Filter the entries by state and open the relevant calculation. Match the AWB or source, qualifying event, recipient context, rule code and version.
2. Read the **Calculation trace** and result. Check the basis, method, rate, limits and saved amount against the approved rule and shipment snapshot.
3. If the entry is **CALCULATED** and your role permits posting, choose **Post to ledger**, read the confirmation and post once. Verify the resulting POSTED state.
4. Inspect the related financial position. Posting accrues Commission Payable; it does not itself make a bank payment. Franchise payable is affected when the relevant settlement is approved.

### Correct a posted error

5. Investigate the source and obtain the required finance authorization before using **Reverse commission** on an eligible POSTED entry.
6. Enter a substantive reversal reason and confirm. The system records the reversing commission and ledger effect while keeping the original evidence.
7. Recheck the entry, ledger and any affected settlement. Ask finance to coordinate further correction where a settlement has already been approved or paid.

**Do not post twice:** an entry already posted by its operational event does not need a second manual posting. If an expected earning is absent, check the event, recipient relationship, rule scope and effective date. Editing a rule does not retroactively rewrite an old calculation.

**Escalation evidence:** AWB, event and time, franchise or unit, rule code/version, calculation reference, expected behavior and actual state. Distinguish a missing rule from a failed financial posting; they have different remedies.

<!-- page -->
## Review the ledger journals and trial balance

**Where:** Finance → Ledger. The chart of accounts links to statements, Journals and Trial balance. Balances come from posted records; the ledger is not a spreadsheet for editing totals.

### Follow an amount to its source

1. In **Chart of accounts**, search by account code or name and filter by account type. Read the balance in the context of that account's normal debit or credit balance; a positive figure does not always mean cash received.
2. Open an account and choose the statement **From** and **To** dates. Review dated movements, references and balances. Follow the related journal where available.
3. In **Journals**, filter by status and source type and open the transaction. Check the reference, account lines, debits, credits and description.
4. Confirm that total debit equals total credit. Use the source reference to investigate a shipment, commission, settlement or billing question instead of guessing from the amount alone.

### Use reversals instead of editing posted entries

5. If finance authorizes a correction and the journal is eligible, use **Reverse journal**, enter the substantive reason and confirm once.
6. Verify the reversal and retained original record. Coordinate the correction with the source workflow; reversing a journal is not automatically a complete correction of its shipment, invoice or settlement.

A POSTED journal is immutable. The current ledger screens focus on account and journal inspection, trial balance and permitted reversal. Do not expect a general account-creation, manual-journal-entry or accounting-period-close wizard here; use the authorized finance process for unsupported controls.

### Review the accounting position

7. Open **Trial balance** and select the **As of** date. Review account totals and the overall debit and credit totals.
8. Inspect **Accounting periods** and their status. Closed periods reject postings. If a valid correction belongs to a closed period, ask finance how it must be handled; do not change the date to bypass the control.
9. If the chart or trial balance shows a **Ledger integrity incident** or imbalance, retain the date and references and escalate to finance and technical support. Do not create an unrelated balancing entry to hide it.

**Result to retain:** the account, statement period, journal/source reference, debit and credit explanation, and any approved corrective record. Use Report centre for the available finance exports rather than interpreting a short register page as a full-period extract.

<!-- page -->
## Generate approve and pay franchise settlements

**Where:** Finance → Settlements. A settlement combines eligible franchise earnings, liabilities and other source lines for a period and states who owes whom.

1. Select **Generate settlement**. Enter the franchise ID, period type, start and end dates, and notes. Review the resulting statement and period before proceeding.
2. In **Summary**, read commission, incentives, COD liability, charges, penalties, adjustments, tax, withholding and opening balance where returned. Use the displayed payment direction, not just the sign of the number.
3. Review **Commission**, **COD**, **Charges** and **Adjustments** source lines. Resolve incorrect or missing sources before approval. Use **Recalculate** while offered for an unfrozen statement, then review the result again.
4. Select **Submit for review** when the CALCULATED statement is ready. A different authorized checker must approve it; the person who calculated it cannot approve their own statement.
5. The checker opens the UNDER_REVIEW statement, reviews the sources and direction, then chooses **Approve** and confirms. Approval posts and freezes the statement totals and lines.
6. After actual payment, an authorized user selects **Record payment** for an APPROVED or PARTIALLY_PAID statement. Enter the amount, mode and reference, then verify the paid amount and status.

### Read the payment direction

**Head Office pays Franchise** means the approved net is payable to the franchise. **Franchise pays Head Office** means the franchise owes head office. A zero net requires no payment. COD held and other liabilities can reduce earnings and change the direction.

Recording payment documents a real payment; it does not instruct a bank to transfer money. Confirm the external receipt and keep its reference. Do not change a sign or use a different franchise to make the payment fit.

### Review the available evidence

**Approval** shows the available calculation and approval progression. **Payments** shows the payment position. The current **Ledger** and **Audit** tabs do not supply complete journal links or detailed audit records, and payment detail fields are incomplete. Ask finance for the supporting evidence when required.

Approved, paid or closed statements are not editable drafts. Corrections require the authorized settlement-adjustment process, which is not fully exposed in this screen. **Cancel** is available only in eligible unfrozen states and requires a substantive reason. Do not regenerate overlapping statements or record a second payment merely to correct an unexplained difference.

<!-- page -->
## Draft issue and settle customer invoices

**Where:** Finance → Billing. The Invoices register contains draft and issued customer billing records and links to the relevant detail.

1. Select **Draft invoice**. Enter the customer ID, billing mode and period. The draft draws from saved shipment charge snapshots; it does not allocate a final statutory number or post money yet.
2. Open the draft and review the customer, period, included shipments, shipment count, line charges, tax components and totals. Confirm that the correct transportation payer is being billed.
3. Resolve source problems before issue. When authorized, use **Issue invoice**, read the confirmation and confirm once. Issue allocates the invoice number and posts the financial record; its header and lines then become immutable.
4. After receiving an actual customer payment, choose **Record payment**. Enter amount, mode and a unique real payment reference. The backend rejects overpayment and recognizes a repeated reference; do not invent new references to retry the same receipt.
5. Verify the resulting payment state and outstanding amount in the saved invoice and related financial records.

### Raise a correction through a credit or debit note

6. On an eligible issued invoice, use the note action. Select CREDIT or DEBIT, the reason code, amount before tax, tax amount where applicable and a substantive reason. Select **Raise draft note**.
7. Retain the draft reference and arrange review by a different authorized checker. The maker cannot issue their own note even if an issue button is visible.
8. Coordinate with finance or implementation support for the checker handoff. The current interface has no complete note list or detail retrieval workflow for another user's draft. Do not share a login or create a duplicate note to work around this gap.
9. Confirm issue and posting through the supported process and recheck the invoice balance. A raised draft note has not changed the ledger yet.

### Know the billing boundaries

Invoice PDF rendering is currently unavailable. The billing screen does not provide a complete billing-run workflow or an unbilled-shipment queue. Large invoices load their line response before the screen paginates the display, so use a suitable billing period and report workflow when reviewing volume.

Where a register has no reliable next-page control, do not assume the visible rows are the complete account history. Use appropriate reports or ask finance for a complete extract. An issued invoice, payment, credit note and physical cash handover remain separate records and should all be reconciled to their real evidence.

<!-- page -->
## Choose and run the right report

**Where:** Reports → Report centre. The available catalogue is returned for your access. Financial reports need the additional finance reporting permission.

| Report group | Available report subjects |
| --- | --- |
| Operations | Shipment volume, branch performance, service performance |
| SLA and franchise | SLA, franchise performance |
| Nigeria finance | Revenue, Nigeria VAT, Nigeria withholding tax, profit and loss, balance sheet, cash and bank book, receivables aging, franchise collections |
| Cash and earnings | COD aging, commission, settlement |
| Exceptions | NDR, RTO, exceptions |

1. Select **Report**, **From**, **To** and the available **Format**. Current generation accepts CSV or JSON. Add operating-unit or shipment-status filters when applicable.
2. Confirm the reporting period and scope. Split periods longer than the supported 400 days rather than repeatedly submitting the same invalid request.
3. Run the report and open **Details** in Recent report runs. A QUEUED or RUNNING state means generation continues in the background; you may keep working.
4. Wait for COMPLETED, then **Download** before the stated expiry. Finished artifacts have a 24-hour download window. Retain an approved copy if needed for your records.
5. Review the report type, period, scope and row count. Check the column labels and money units before using amounts in a spreadsheet; machine-oriented exports may use minor units rather than displayed currency amounts.

### Handle the result state

For FAILED, open details and read the reason, then correct the request or escalate with the run reference. For EXPIRED, create a new run with the correct filters. Use **Cancel** only for a queued or running job you no longer need; a completed artifact is not cancelled this way.

The interface has no full browser preview and no percentage-complete estimate. A running state alone is not evidence of a fault. If a downloaded file does not open as expected, retain its run reference and requested format and ask support; do not assume its filename alone establishes the content format.

**Financial reports:** use the configured classifications and actual recorded postings. A tax-labelled export is not an electronic tax filing or proof that every source transaction is complete. Finance must review it using the approved accounting process before external submission or decisions.

<!-- page -->
## Use the command centre to direct work

**Where:** Operations → Command centre. Managers use this view to decide where investigation or action is needed, then open the appropriate operational or financial records.

1. Select **Today**, **Yesterday**, **7 Days** or a custom date range. Check any operating-unit and service filters and the refresh time.
2. Read period activity alongside current status and backlog. The period totals describe activity in a chosen interval; live backlog describes what remains open now.
3. Review delivery and SLA indicators, NDR reason groups, open exceptions and COD aging. Identify the responsible branch, hub or finance owner before assigning follow-up.
4. Open the relevant queue or record and verify the underlying AWBs, dates, statuses and custody before taking action. Use Report centre when a complete bounded export is needed.
5. Refresh after confirmed operational changes and consider the snapshot timestamp. A cached or sampled view is not a promise of second-by-second movement.

### Read the labels carefully

**Pickup pending** here represents uncollected bookings; it is not a dedicated count of appointments late against their pickup window. The NDR view groups reasons; it does not provide a ranked NDR-aging queue. Distinct franchise pickup, bag, manifest and inbound or outbound counters are also not all present in the franchise summary.

If the screen says a view is sampled, investigate the available records without treating the sample as a complete population. Use the proper export or queue to establish the full set of affected records.

### Make a management follow up useful

For hub backlog, assign an owner to review expected inbound, receipts, custody and discrepancies. For delivery exceptions, review attempts and the current NDR plan. For COD aging, have finance verify the holder and pending handover rather than directing another delivery scan.

Record the issue, accountable owner, relevant references, next action and agreed review time. The dashboard is an observation point; it does not itself fix a failed delivery, transfer cash or approve a return.

**Why two numbers may differ:** a report and dashboard can use different periods, scopes, status definitions, snapshot times or live-versus-period measures. Align those first. If the difference remains, give support both filter sets, timestamps and references rather than only two screenshots of totals.

<!-- page -->
## Close the shift and hand over accountable work

Use the applicable checks at the end of each shift and assign an owner to every unresolved issue. A verbal handover without references makes the next team repeat the investigation.

### Operations checks

- **Counter:** account for unbooked parcels, incomplete bookings, unreadable labels, pending customer decisions and any uncertain save response.
- **Pickup:** review unassigned and failed visits, expected versus collected pieces and work still with an agent.
- **Warehouse:** reconcile recent accepted scans with physical parcels. Record open bags, closed bags awaiting movement, manifests awaiting closure or receipt, and seal discrepancies.
- **Transport and receiving:** identify departed trips, arrived trips awaiting receiving, incomplete reconciliations and unresolved missing, excess, damaged or misrouted items.
- **Delivery:** check incomplete stops, failed attempts, parcels still with agents, current NDR plans and returns awaiting sender handover.
- **Evidence and support:** identify missing POD or unresolved customer enquiries and give the responsible team the AWB and last confirmed event.

### Finance and administration checks

- Count actual cash and verify non-cash references against the correct collection and COD records. Record shortages or excesses and pending acceptance of handovers.
- Review invoices and settlements waiting for approval or payment. Check that recorded payments correspond to real receipts and are not duplicates.
- Investigate missing or failed commission postings and any ledger integrity alert. Keep accounting corrections tied to their source records.
- Review failed customer notifications, paused partner endpoints and failed or expiring reports that affect the next shift's work. Assign the appropriate administrator or integration owner.

### Write one useful handover entry per issue

Use: **Reference; current physical location or money holder; system status; last confirmed action and time; observed issue; next action; named owner; agreed time.** Add the container, run, invoice or settlement reference where relevant.

Example for training: “AWB [reference], at [branch], receipt confirmed at [time]. One item has a damaged outer carton. Exception [reference] is open. [Owner] will inspect and record the approved action before onward dispatch.”

If a connection failed during a booking, scan or payment, state that the saved outcome is uncertain and identify where the next team must check. Do not describe a planned handover as complete. Save confirmed work, secure paperwork and cash, and sign out on shared workstations.
