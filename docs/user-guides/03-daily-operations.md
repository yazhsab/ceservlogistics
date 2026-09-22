# CESERV Daily Operations Guide

For counter teams, pickup and delivery staff, dispatchers, hubs and finance

Use this guide alongside your assigned work queue. Record what physically happened, at the correct facility, and wait for the application's confirmation before moving to the next action. Your role and the current shipment state determine which controls are available.

**Edition:** 18 September 2026. This guide describes the implemented application workflow. Your supervisor sets local cutoffs, cash-handling procedures, delivery promises and escalation contacts.

### Begin the shift

1. Sign in at https://app.ceservlogistics.com/login with your own staff account. Confirm your operating access in **My profile** and select the correct facility wherever a screen requests it.
2. Review the work assigned to your role: pickups, expected inbound, dispatch, destination queue, delivery runs or financial exceptions.
3. Check the date and status filters before deciding a queue is empty. Verify that a scanner and label printer are ready if your job uses them.
4. Review the previous shift's unresolved parcels, missing scans, seal discrepancies, cash differences and pending handovers with the supervisor.

### Go directly to your task

| Task | Page |
| --- | --- |
| Counter work and pickup runs | 2 |
| Scanner operation | 3 |
| Bags and manifests | 4 |
| Trips and hub receiving | 5 |
| Destination sorting and delivery | 6 |
| Failed delivery and returns | 7 |
| Collections and finance | 8 |
| Reports and shift handover | 9 |

### Know the movement you are recording

A typical parcel moves through booking, pickup or origin receipt, outbound handling, line haul, destination receipt, dispatch for delivery and final delivery. Bags, manifests and trips group that movement. Each stage has its own record; marking a vehicle arrived does not receive every parcel, and adding a parcel to a delivery run does not mark it out for delivery.

**Use the right reference:** AWB identifies the shipment. Customer code identifies the account. Bag, manifest and run codes identify their own records. Fields explicitly labelled **ID** may require a different reference; obtain it from the responsible administrator rather than guessing.

<!-- page -->
## Book parcels and arrange pickups

### Counter checklist

1. Search for the customer. If no active account exists, create one under **Commercial → Customers** when authorized. In **New booking**, click the customer search result.
2. Confirm sender and recipient addresses, country and postal code. Record the real package weight and dimensions, contents and declared value.
3. Agree the service, payment mode, transportation payer, duty payer and insurance decision. Add customs goods lines when required.
4. Preview, review the charge breakdown, obtain acceptance of the current insurance premium if requested, and select **Confirm & book** once.
5. Check the AWB and label. Keep payments separate from booking confirmation: a successful booking does not itself prove money was received.

The Getting Started and Booking Guide provides the detailed booking walkthrough. If a save response is uncertain, check **Shipments** before creating a second booking.

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

Open and reconcile contents only through the actions offered for the current state. A container receipt and a complete item count are separate checks. The Hub operations instructions on page 5 explain reconciliation.

**If an action is missing:** review the container status, destination, your facility and your permission. A closed declaration cannot be treated as an editable draft simply because a parcel was forgotten.

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
## Record collections and review finance

### Keep the different money records clear

| Record | What it records |
| --- | --- |
| Customer collection | Shipment payment received at the franchise or origin, such as prepaid cash, POS or transfer |
| COD | Money collected from the recipient and the subsequent custody of that money |
| Invoice payment | A payment applied to an issued customer invoice |
| Settlement | The approved net amount due between head office and a franchise |

**Customer collections:** where your franchise role permits it, open **Finance → Customer collections**. Enter the correct shipment ID, full shipment payment, method and transaction reference when required. Compare the amount with the shipment total and verify the saved collection. Do not enter a COD collection here merely because cash was received.

### Review and reconcile COD

1. Open **Finance → COD control** and find the obligation by AWB. Review expected and collected amounts, current holder and custody timeline.
2. Use **Check custody** to compare the relevant party's position. Use **Reconcile custody** when authorized, identifying the correct party and period.
3. Count and record the actual amounts, notes and any variance reason. Investigate shortages and excesses instead of changing the booked expectation to match cash in hand.
4. Follow the approved cash handover process. The current holder remains responsible until the next party accepts the handover. Confirm the recorded result with finance.

The current COD screen does not expose every transfer, remittance or dispute queue. If a required handover action is unavailable, ask finance or implementation support to complete the supported workflow; do not assume reconciliation itself transferred money.

### Billing and settlements

Under **Finance → Billing**, create the authorized invoice draft for the correct customer and period, review its shipments, taxes and totals, then issue only after approval. Issuing makes it immutable. Record a real payment with its amount, method and unique reference; corrections use the permitted credit or debit note process. Invoice PDF rendering is currently unavailable.

For **Settlements**, generate and review the franchise period and its source lines before submission and approval. Read the displayed direction of payment: head office pays the franchise, or the franchise pays head office. Record actual payments through the authorized workflow. Approved or paid records are not editable working drafts.

**Ledger and commission:** review the reference, rule, amount and status on the saved entry. Escalate discrepancies with those references. Use approved reversal or adjustment processes for posted entries; do not create an unrelated balancing transaction to hide a difference.

<!-- page -->
## Run reports and hand over the shift

### Produce a report

1. Open **Reports → Report centre** and choose the report appropriate to your work.
2. Set **From**, **To**, **Format**, and any operating-unit or shipment-status filter. Check the period and branch before running it. Financial reports require the corresponding permissions.
3. Start the report and open its run record. Wait while it is queued or running; the screen does not provide a browser preview of the full export.
4. When complete, download the result. Check that its period, filters and row count match the request before using it for decisions or sharing it through an approved channel.
5. If it fails, read the error. Correct the request or give support the run reference. Finished downloads expire after 24 hours; run a new report if the download window has ended. Split a period longer than 400 days.

### Read the command centre in context

Use the **Command centre** to identify work needing attention, then open the corresponding queue or record. Check the period, selected scope, refresh time and any sampled or live-backlog label before comparing a number with a downloaded report. A live queue and a report for a historical period answer different questions.

### End of shift checklist

- **Parcels:** account for unbooked parcels, missing labels, uncollected pickups and parcels that were scanned physically but not confirmed in the application.
- **Containers and trips:** record open bags, unclosed manifests, departed or arrived trips awaiting receipt, and unresolved seal or count differences.
- **Delivery:** review incomplete stops, failed attempts, NDR actions, parcels still with agents and returns awaiting sender handover.
- **Money:** reconcile the actual cash and payment references against the relevant customer collection and COD records. Record and escalate differences and unaccepted handovers.
- **Follow-up:** give the next shift the references, current holder or location, unresolved issue, named owner and next agreed action. Confirm that urgent cases have an owner.
- **Account:** save confirmed work and sign out, especially on a shared counter or warehouse computer.

**A useful handover entry:** “AWB [reference], at [facility or agent], [current status]. Issue: [observed problem]. Last confirmed action: [time and action]. Next action: [owner and agreed time].” Use real references; do not include passwords or OTPs.

**When blocked:** keep the physical parcel or cash accountable, preserve the error and contact the responsible supervisor. The Common Questions and Quick Reference guide includes a support checklist.
