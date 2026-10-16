# Questions for compliance counsel

**Status:** to be sent. Answers must be in writing; a verbal answer cannot be cited as a `source_reference`.
**Date:** 2026-10-16
**Purpose:** to obtain the authoritative limits, tier definitions, and retention rules that the KYC work depends
on. **No limit will be implemented before these are answered.** Every figure currently in the design is marked
UNCONFIRMED because nothing has been verified against a primary source.

**What counsel needs to know about the product to answer:**

- Agro-Shield is a Nigerian agricultural marketplace. It holds a **prefunded NGN wallet** per user.
- Money **in** is proposed via a payment provider; money **out** is a payout to the user's own bank account and
  is already live.
- Users can also **pay each other from wallet to wallet** within the marketplace, and pay for marketplace orders.
- Every account today is an **individual**. There is **no company, business, or CAC-registered entity** in the
  data model — see Q14, which may change this.
- There is currently **no identity verification of any kind** and **no transaction limit of any kind**.

---

## Part 1 — Which regulatory regime applies

This determines the answers to everything else, so it must be settled first.

**Q1.** Given the description above, which regime governs the wallet — the CBN **Payments System Vision / licensing
categories** (MMO, PSSP, PTSP, Switching), the **agent banking** guidelines, the **Guide to Charges**, or something
else? Please name the instrument and cite the paragraph.

**Q2.** Does the arrangement where money in and money out are handled by **two different providers** change which
licence or regime applies? If the wallet is not itself a licensed product, is the regulated activity the
**movement of funds** rather than the stored balance?

**Q3.** Does the **wallet-to-wallet transfer** between users make Agro-Shield a **payment service provider** in its
own right, even though the aggregate funds never leave the provider ecosystem? This is the question most likely to
change the product's legal posture.

**Q4.** Is there any **threshold** — user count, transaction value, float size — above which a licence or
registration becomes mandatory? If so, what is the current value and where is it stated?

**Q5.** Does **NDPA / NDPC** registration apply to Agro-Shield as a data controller processing identity data?
What is the **current** registration requirement and fee, and is there a **data controller** registration deadline
we have missed?

## Part 2 — KYC tiers and limits

**Every answer to this part must cite the governing instrument and paragraph, or state that no such requirement
exists.** A figure we cannot cite cannot be implemented.

**Q6.** For a wallet of this type, what are the **required KYC tiers**? For each tier please give:

- the **name and definition** of the tier;
- the **documentary or electronic evidence** required to reach it;
- the **single-transaction limit**, separately for **money in** and **money out**;
- any **daily, weekly, and monthly cumulative limits**, again separately by direction;
- any **maximum balance** the wallet may hold.

**Q7.** Are these limits **mandated by regulation**, or are they **guidance/industry practice** that a PSP may set
differently with justification? If the latter, we will set our own **stricter** values and document the reasoning
rather than inventing a regulatory basis that does not exist.

**Q8.** **Tier 1 specifically:** is there a **tier with no identity verification**, and if so what may such a user
do? We currently have no verified users at all, so the entire existing user base would land in whatever the lowest
tier is.

**Q9.** Is a **BVN or NIN required** to open or operate this wallet at all, or is it required only above a
threshold? If required, is **one** sufficient or are **both** required?

**Q10.** Are the limits defined per **wallet**, per **user**, or per **identified person**? This matters because a
person could otherwise hold two accounts and double their effective limit.

**Q11.** **Refunds and reversals:** when a payout or transfer is rejected and the money returns to the wallet,
**does that day's outgoing total go back down?** i.e. is the daily limit **gross** (every debit counts) or **net**
(reversals restore the allowance)? This has to be answered before limit accounting can be written — the two
readings give different answers and different compliance exposure.

**Q12.** **Pending transactions:** does a **pending incoming** deposit count toward that day's **receiving** limit,
or only once it settles? Our outgoing operations are reservation-based (money leaves the balance at request time),
so pending outgoing is already counted; pending incoming is the open question.

**Q13.** **Window definition:** what exactly is "a day" for these limits — **calendar day in WAT**, a rolling
24-hour period, or a **business day**, and does it reset at midnight local time? Please state the timezone
explicitly. (We will not assume UTC+1 merely because it is plausible.)

**Q14.** **Corporate accounts:** if a CAC-registered business, cooperative, or farmer group wanted a wallet:

- what **additional** verification is required beyond an individual's;
- does an **individual's** NIN/BVN verification extend any authority to a business, or must the business be
  verified as a separate legal person;
- who is the **beneficial owner** that must be identified;
- does a CAC certificate by itself grant any **financial-service** or wallet authority?

We currently have no organisation entity in the data model, so the answer determines whether one is needed.

**Q15.** Does the **age** of the account holder affect permissible limits? If a **minor** may hold a restricted
wallet, what are the specific limits and what parental/guardian consent is required in what form?

## Part 3 — Identity data handling

**Q16.** Are we **prohibited** from storing an **unmasked BVN or NIN**? If storage is permitted, what **specific**
protection is required — encryption at rest, a specific algorithm or key custody arrangement, access logging,
segregation? **We intend to store only a masked identifier and the provider's verification reference, and never
the raw value.** We want that position confirmed as compliant rather than merely prudent.

**Q17.** What is the **permitted retention period** for identity verification records, and for the **verification
reference** we receive from a provider? For how long must we keep them for **AML/record-keeping** purposes, and
after that must we **delete** them?

**Q18.** What **consent** must we obtain before verifying a user's BVN or NIN, and **what evidence of that consent**
must we retain? Does the provider's own consent mechanism satisfy this, or do we need our own record?

**Q19.** Are there restrictions on verifying the **same** identifier more than once, or on verifying an identifier
that belongs to someone who is **not** the account holder?

**Q20.** What **NDPA** obligations attach to the identity data specifically: is a **DPIA** required, must the data
be stored **within Nigeria**, and what must our **privacy notice** say about it? Is there a **breach notification**
period we must meet?

**Q21.** How should a user's identity data be handled on **account deletion**? Given the retention question (Q17),
what must be deleted immediately and what must be retained, and for how long?

## Part 4 — Operations and disputes

**Q22.** What **suspicious-transaction reporting** obligations apply, to which agency, within what period, and does
our transaction-monitoring design need to **preserve records** in a particular form?

**Q23.** What must we do when a user's wallet is **restricted or suspended** while it holds a **non-zero balance**?
Are we permitted to hold the funds, and for how long, and what must we tell the user?

**Q24.** Is there a required **complaint/dispute** procedure and timeframe for wallet issues, and must it be
published?

**Q25.** What **records** must we keep for the wallet specifically, and for how long — the ledger, the limit
decisions, and the admin actions? Our design currently:
(a) keeps an append-only ledger, (b) records which policy row produced each limit decision, and
(c) proposes an admin audit log with actor, timestamp, reason, and prior state.
Please tell us whether that is sufficient or whether something is missing.

**Q26.** **Existing users with real balances.** When KYC gating is introduced, users who already hold money may be
unable to withdraw it until they verify. What is the legally safe approach — a grace period, a
**withdraw-only** mode, a hard block — and for how long?

---

## What we are doing in the meantime, and what we are not

**Doing:** designing the **policy storage** — limits as data rows, each carrying a `source_reference`,
`effective_from`, direction, and window — so the confirmed numbers can be entered the moment they arrive. Building
nothing that depends on a specific number.

**Not doing, until these answers arrive:**

- No **invented** limit values, tier boundaries, or balance caps. Every current figure in the design is
  **UNCONFIRMED** and no such value exists in code.
- No **description of the product as compliant** — we do not yet know what compliance requires.
- No **raw NIN or BVN storage**.
- No **live verification** against real people's identifiers.
- No assumption that **NIN possession proves adulthood**, that a **CAC certificate** grants financial-service
  authorisation, or that a **successful payment** proves identity.
