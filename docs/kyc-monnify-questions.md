# Questions for Monnify

**Status:** to be sent. Answers must be in writing (email or support ticket) — a verbal answer cannot be cited as a `source_reference` in the policy table.
**Date:** 2026-10-16
**Why this document exists:** the funding provider is *proposed* Monnify but the merchant account's enabled products are unknown, and the official API documentation is unreachable from the development environment. Nothing about the Monnify integration can be designed until §A and §B are answered in writing.

**Context to give Monnify when sending:** Agro-Shield is a Nigerian agricultural marketplace operating a
prefunded wallet. Users fund a wallet (Money In), then spend it on orders or request a payout to their own bank
account (Money Out). **Money Out is already handled by another provider and is not part of this request.** The
questions below concern Money In and identity verification only.

---

## A. Account and product enablement — must be answered first

Nothing else can be designed until these are resolved, because the shape of the integration differs completely
depending on the answers.

| # | Question | Why it blocks design |
|---|---|---|
| A1 | Which Monnify products are **actually enabled** on our merchant account, listed explicitly? Specifically: Checkout (hosted), Reserved/Dedicated Virtual Account, Bank Transfer collection, Direct Debit, and Identity Verification (BVN / NIN). | A Merchant Account being approved for payments does **not** imply the dedicated-virtual-account or identity-verification products are switched on. We will not assume. |
| A2 | For **Money In**, which method does our account support: a **standing reserved account** the user transfers to repeatedly, or a **per-transaction hosted checkout**? | This decides whether there is a per-attempt "start funding" call at all. With a reserved account there is none — money arrives unprompted and we match on reference and amount. With checkout there is. Designing the wrong one wastes the work. |
| A3 | Is there a **sandbox** for our account, and are all enabled products available in it? What are the sandbox base URL and credentials? | We must not build against production, and we must not run live verification with real customer identifiers. |
| A4 | Are **Webhooks** enabled, and can we register our own URL and a secret? | Confirmation must be server-side. |
| A5 | Do our account and the requested products require **any additional KYB/KYC paperwork on Agro-Shield itself** before they can be enabled? | An account requirement we have not met would silently block launch. |
| A6 | Does our account have a **settlement account** configured, and does settlement go to a bank account we control? | Needed before we can hold user funds responsibly. |

## B. Money In — mechanics once A2 is answered

Answer the branch that applies; the reserved-account branch and the checkout branch are not the same questions.

### If Money In is a **Reserved / Dedicated Virtual Account**

| # | Question |
|---|---|
| B1 | Does each user get a **unique** account number, or are all users given the **same** account and distinguished only by the payment reference? |
| B2 | If unique per user: what is the API call to create one, what are its request and response fields, and is the account number **permanent** or does it expire? |
| B3 | If shared: how exactly is an incoming transfer matched to a user — by a mandatory reference prefix, by the transfer narration, by amount, or some combination? What is the exact matching algorithm, including what happens on an ambiguous or unmatched payment? |
| B4 | What does the **webhook payload** for a successful incoming bank transfer contain, field by field? We specifically need: the unique transaction reference, the amount, the currency, the sender name, the sender account number, the receiving account, the payment status, and the timestamp with its timezone. |
| B5 | Is there a **transaction-status/query** endpoint to reconcile a payment we did not receive a webhook for? Give the endpoint, the request, and the response. |
| B6 | What is the **minimum and maximum** single incoming transfer our account accepts? Is there a **daily or per-account receiving cap** imposed by Monnify itself, separate from any limit we impose? |
| B7 | How are **reversals** represented? If a payment is credited and later reversed by the sender's bank or by Monnify, is there a webhook, which status value, and does it carry the original transaction reference so we can match it? |
| B8 | Can a transfer arrive with an amount that **differs** from what we expected? How should we treat an overpayment or an underpayment? |

### If Money In is **Hosted Checkout**

| # | Question |
|---|---|
| B9 | What are the exact request fields for initiating a checkout, and what is the response — a URL we redirect to, or an iframe? |
| B10 | What identifies the transaction in the **redirect** back to us, and what identifies it in the **webhook**? Are they the same value? |
| B11 | Can we set our own unique reference, or does Monnify generate it, or both? Which value is guaranteed unique on our side? |
| B12 | What does the webhook payload contain for a successful payment, and for a failed/abandoned one? |
| B13 | What is the **session/expiry** behaviour? If a user opens checkout and never pays, how long does the reference stay live, and can it be paid twice? |
| B14 | Minimum and maximum single checkout amount for our account? |

### Common to both

| # | Question |
|---|---|
| B15 | **Webhook security:** what exactly is the signature or hash scheme, which HTTP header carries it, is it computed over the **raw request body** or a canonicalised form, what is the **encoding** (hex or base64), and is the shared secret per-merchant or per-endpoint? **We will not implement a signature check from a guess — a wrong one is a silent authentication bypass.** |
| B16 | **Webhook retries:** how many times is a webhook retried after a non-2xx response, over what period, and what response code and body do you expect for success? |
| B17 | **Webhook duplicates:** are we guaranteed at-least-once delivery, and can the same event be delivered **concurrently** (i.e. two in-flight deliveries of the same event before either is acknowledged)? |
| B18 | Are webhooks **signed and sent from published IP addresses** we can allowlist? If so, provide the ranges for both sandbox and production. |
| B19 | **Fees:** what is charged per Money In transaction, is it deducted from the amount or billed separately, and does the webhook amount represent the gross the user sent or the net we receive? |
| B20 | **Timezones:** in what timezone are the timestamps in the API and webhook payloads, and are they ISO-8601 with an offset? |

## C. Identity verification — BVN and NIN

**Not required to launch wallet funding**, but relevant to the KYC work. Treat as a separate request.

| # | Question |
|---|---|
| C1 | Is **BVN verification** enabled on our account? What are the endpoint, request fields, and response fields? |
| C2 | Is **NIN verification** enabled? Which NIN variant — the number lookup, the NIMC virtual NIN (vNIN), or a tokenised form? |
| C3 | For each: does the response include the **date of birth**, and is it the **verified** value from the source registry or a pass-through of what the caller supplied? |
| C4 | Does the response include the person's **name** as recorded at the registry, and can it be matched against our stored name? What matching rules do you apply? |
| C5 | What is the **consent** requirement? Is a user-supplied authorisation, an OTP, or a specific consent string required per verification, and are we expected to **store** any consent evidence? |
| C6 | What are the **per-verification fees**, and are they charged on failure? |
| C7 | Are we permitted under our agreement to **store** the verification result and reference, and are we prohibited from storing the raw NIN/BVN? **Please state the contractual position in writing** — we intend to store only a masked identifier and the provider reference, and we want that confirmed as acceptable. |
| C8 | **Rate limits** on the verification endpoints: requests per second/minute, per day, and per identifier. |
| C9 | Is there a **sandbox** with **synthetic** test identifiers? We will not run live verification against real people's identifiers during development under any circumstances. |
| C10 | Does a **successful NIN verification** imply anything about the person's age or legal capacity, or is it strictly an identity match? **We will not treat a successful verification as proof of adulthood.** |
| C11 | What is returned when a verification **fails** or the identifier is not found — a distinct error code, a 404, or a 200 with a failure flag? We must never read a failure as a success. |
| C12 | Do you retain the identifier we send, for how long, and is it used for any purpose beyond answering our request? |

---

## What we are **not** asking Monnify

- We are **not** asking Monnify to define our wallet limits or KYC tiers. Monnify's account-level caps (B6/B14)
  are a constraint we must fit inside, not the regulatory limit itself. The regulatory limits come from
  [kyc-counsel-questions.md](kyc-counsel-questions.md).
- We are **not** asking to replace Flutterwave for payouts. Money Out stays where it is.

## How the answers will be used

Every confirmed figure — a cap, a fee, a retry schedule — will be recorded in the `kyc_policies` table with a
`source_reference` naming the document and date it came from. **No limit will be hardcoded in Go.** Until an
answer arrives in writing, the corresponding row does not exist and the corresponding behaviour is disabled,
not guessed.
