# AGRO-SHIELD
## Proposed Product Development Plan

**Tagline:** Protecting Every Harvest. Connecting Every Farmer.

**Project Category:** Agricultural Technology (AgriTech), Artificial Intelligence, Digital Marketplace, and Financial Technology

**Product Vision:** To build a reliable agricultural platform that helps farmers protect their harvests, learn better farming practices, identify farming problems, access market opportunities, and manage their agricultural businesses.

**Target Users:** Crop farmers, livestock farmers, poultry farmers, fish farmers, horticultural farmers, mixed farmers, produce buyers, cooperatives, storage operators, and other agricultural stakeholders.

---

## 1. Our Main Objectives

Agro-Shield aims to:

1. Help farmers reduce post-harvest losses.
2. Connect farmers directly with produce buyers.
3. Provide useful market prices and price insights.
4. Make agricultural education and problem diagnosis accessible through AI.
5. Give farmers access to weather information and local agricultural insights.
6. Support farmers who use basic phones through USSD.
7. Provide a secure wallet and payment system for agricultural transactions.
8. Build a platform that can grow to support farmers throughout Nigeria and, eventually, other countries.

## 2. Proposed Core Features

### Feature 1: Farmer Registration, Authentication, and Profiles

**Purpose:** Give every farmer a secure and reliable account.

Proposed capabilities:

- Registration and login using a phone number or other approved identification method.
- Secure password handling and account recovery.
- Phone-number verification using a reliable SMS or other approved verification provider.
- Farmer profiles containing personal details, location, farming type, and relevant farm information.
- Mandatory profile photograph, according to our proposed registration requirements.
- Support for Nigeria's states, the Federal Capital Territory, and all 774 Local Government Areas.
- Role-based access for farmers, buyers, administrators, and other approved users.
- Appropriate account security, session management, and rate limiting.

**Expected result:** Users can register, verify their accounts, log in, and manage their profiles reliably.

### Feature 2: Agricultural Marketplace

**Purpose:** Connect farmers who have products to sell with people who want to buy them.

Proposed capabilities:

- Farmers can list agricultural products.
- Each listing includes a product name, description, quantity, price, location, and at least one product photograph.
- Support for crops, livestock, poultry, fish, and other approved agricultural products.
- Buyers can search, filter, and compare available products.
- Buyers and sellers can negotiate where supported.
- Order creation, order tracking, and transaction history.
- Payment processing and seller payouts through approved payment providers.
- Delivery or logistics coordination.
- Clear order statuses and a process for handling cancellations, disputes, and refunds.

**Expected result:** A farmer can list a product, a buyer can place an order, and both parties can follow the transaction to completion.

### Feature 3: Smart Storage and Post-Harvest Protection

**Purpose:** Help farmers and storage operators protect agricultural produce from spoilage and avoidable losses.

Proposed capabilities:

- Register and manage storage facilities.
- Record the type and quantity of stored produce.
- Track storage dates and expected storage duration.
- Monitor temperature and humidity where suitable sensors or reliable data sources are available.
- Provide alerts when measured conditions exceed defined safe limits.
- Identify possible spoilage risks and recommend preventive actions.
- Maintain storage records and historical reports.

**Implementation approach:** Begin with reliable manual records and available data. Add real sensor integration when suitable hardware and infrastructure are available. Simulated readings must be clearly labelled as simulated.

**Expected result:** Users can monitor stored produce and receive useful warnings before avoidable losses occur.

### Feature 4: Market Prices and Price Prediction

**Purpose:** Help farmers make better decisions about when and where to sell their products.

Proposed capabilities:

- Display agricultural commodity prices.
- Compare prices across available markets and locations.
- Track price changes over time.
- Show historical price trends.
- Provide estimated future prices when enough reliable data is available.
- Explain the factors that may influence price changes.
- Offer practical selling insights without presenting predictions as guaranteed prices.

**Expected result:** Farmers can compare market opportunities and make better-informed selling decisions.

### Feature 5: Agro-Shield AI Assistant

**Purpose:** Give farmers access to agricultural education and practical support.

The AI system will have two separate flows.

**A. Learning Assistant**

- Teach users according to their selected farming type.
- Use simple, friendly, patient language.
- Explain farming concepts step by step.
- Support crop farming, livestock, poultry, fish farming and aquaculture, mixed farming, and horticulture.
- Maintain relevant recent conversation history.
- Use Groq as the primary provider.
- Use OpenRouter as the backup provider.
- Must not use Gemini for the Learning Assistant.

**B. Diagnosis Assistant**

- Help users investigate farming problems.
- Accept descriptions of symptoms and relevant photographs where supported.
- Provide possible causes, practical next steps, and appropriate warnings.
- Clearly explain uncertainty and recommend qualified agricultural or veterinary help when necessary.
- Use Gemini as the diagnosis provider, according to our agreed architecture.
- Handle provider failures and incomplete responses safely.

**Expected result:** Learning remains an educational experience, while Diagnosis remains a separate problem-investigation experience.

### Feature 6: Weather and Location Intelligence

**Purpose:** Help farmers understand weather conditions that may affect their farms.

Proposed capabilities:

- Support all 36 Nigerian states and the Federal Capital Territory.
- Support all 774 Local Government Areas in the location data model.
- Provide weather information for supported locations.
- Display forecasts when a reliable weather provider supplies them.
- Provide relevant weather alerts and farming guidance.
- Connect weather insights to appropriate agricultural activities.
- Make relevant weather information available to other supported Agro-Shield channels, including USSD where feasible.

**Expected result:** Farmers receive useful location-based weather information without being shown invented or unavailable forecasts.

### Feature 7: USSD Access

**Purpose:** Make essential Agro-Shield services accessible to users without smartphones or reliable internet access.

**Proposed USSD code:** *347# — subject to code availability and approval by the relevant provider.

Proposed menu options:

- Check selected market prices.
- View supported weather information.
- Access basic farm or storage information.
- Receive relevant notifications.
- Find available buyers or register selling interest.
- Check order information where supported.
- Access basic account services.

The first version should prioritise essential, read-only information. Transactions, account changes, and other sensitive operations should be added only after proper identity checks, session handling, provider integration, and security testing.

**Expected result:** Farmers using basic phones can access selected services without needing a smartphone application.

### Feature 8: Individual User Wallet and Payments

**Purpose:** Support secure payments within the Agro-Shield marketplace and make transaction records easy to understand.

Proposed capabilities:

- An individual wallet for each eligible user.
- Wallet balance and transaction history.
- Wallet funding through approved payment channels.
- Verified payment confirmation before crediting a wallet.
- Payment for marketplace orders where supported.
- Secure withdrawals to verified personal bank accounts.
- Bank-account name verification before a withdrawal is submitted.
- Recipient verification for transfers between Agro-Shield users.
- Clear processing, successful, failed, expired, and reversed transaction statuses.
- Idempotent transaction processing to prevent duplicate credits.
- Transaction records, reconciliation, refunds, and dispute handling.
- Administrative oversight and appropriate transaction limits.

**Payment-provider approach:** Evaluate wallet-funding and payout providers separately. Confirm the actual products enabled on our merchant account, applicable charges, settlement times, transaction limits, and Nigerian regulatory requirements before selecting the final arrangement.

**Expected result:** Users can fund their wallets, pay for eligible orders, and withdraw money safely, with accurate balances and traceable transaction records.

### Feature 9: Notifications and Communication

**Purpose:** Keep users informed about important activities.

Proposed notifications include:

- Phone verification and account-security messages.
- Order updates.
- Payment and withdrawal updates.
- Storage warnings.
- Relevant weather alerts.
- Marketplace messages and other important account events.

SMS, email, and WhatsApp should be integrated according to provider reliability, user consent, cost, and availability. Critical notifications should have sensible failure handling and delivery records.

### Feature 10: Administration and Platform Management

**Purpose:** Give authorised staff the tools needed to operate Agro-Shield responsibly.

Proposed capabilities:

- Manage users and account status.
- Manage marketplace listings and reported content.
- Review relevant transactions and resolve supported disputes.
- Monitor storage, marketplace, and platform activity.
- Manage approved administrative permissions.
- View important application and security events.
- Maintain an audit trail for sensitive administrative actions.
- Monitor failed jobs, payment events, and external service failures.

Administrative actions must be protected by appropriate permissions and must not expose passwords, OTPs, API keys, or unnecessary sensitive identity information.

### Feature 11: Analytics and Reporting

**Purpose:** Help farmers, cooperatives, and authorised administrators understand agricultural activity.

Proposed reports:

- Marketplace activity and completed orders.
- Produce listings and available quantities.
- Storage records and reported losses.
- Market-price trends.
- Wallet and payment reconciliation reports for authorised staff.
- Platform usage and service reliability.

Reports must be based on real stored records. Any sample or demonstration data must be clearly identified.

### Feature 12: Future Expansion

After the core platform is stable, consider:

- Cooperative and farmer-group management.
- Logistics and delivery partnerships.
- Warehouse and storage-operator management.
- Multilingual support for Nigerian users.
- More advanced price intelligence.
- Additional agricultural data providers.
- Wider regional and international expansion.

These features should be introduced according to user needs, available data, cost, and operational readiness.

---

## 3. Proposed Technical Foundation

Agro-Shield should maintain a simple, organised architecture that can grow without unnecessary duplication.

- **Backend:** Go using the standard `net/http` package.
- **Frontend:** HTML, CSS, and JavaScript.
- **Database:** Neon PostgreSQL as the source of truth.
- **Database access:** `pgx` where appropriate.
- **Configuration:** Centralised environment-based configuration.
- **AI Learning:** Groq primary, OpenRouter backup.
- **AI Diagnosis:** Gemini.
- **External integrations:** Approved providers for SMS, weather, payments, and other required services.
- **Security:** Authentication, authorisation, validation, rate limiting, secure session handling, privacy-conscious logging, and transaction safeguards.
- **Testing:** Unit tests, database integration tests, provider-failure tests, and end-to-end tests.

The system should have clear boundaries between handlers, business services, repositories, external providers, and frontend code. Each part should have one clear responsibility.

## 4. Proposed Development Phases

### Phase 1 — Audit and Foundation

- Resolve the outstanding Git branch and merge issues safely.
- Review the existing codebase and database schema.
- Confirm which features actually work.
- Review authentication, authorisation, privacy, and logging.
- Identify unfinished integrations and failed test coverage.
- Establish a reliable baseline for future changes.

### Phase 2 — Farmer Accounts and Core Marketplace

- Complete registration, login, profiles, and verification.
- Complete product listings, search, and product photographs.
- Verify the order lifecycle from creation to completion.
- Implement or complete order cancellation, dispute, and refund handling.

### Phase 3 — AI, Storage, Weather, and Market Information

- Verify Learning and Diagnosis independently.
- Fix incomplete or truncated AI responses.
- Complete storage records and alert logic.
- Verify weather coverage and location data.
- Implement reliable market-price records before expanding predictions.

### Phase 4 — Wallet and Financial Reliability

- Verify wallet creation, funding, and balance updates.
- Test duplicate payment notifications and concurrent transactions.
- Complete transaction statuses and reconciliation.
- Verify bank-account names before payouts.
- Implement safe failure handling, refunds, and reversals.
- Confirm payment-provider capabilities and required compliance.

### Phase 5 — USSD and Notifications

- Confirm USSD provider availability and the proposed code.
- Implement and test essential USSD menus.
- Complete notification delivery records and failure handling.
- Ensure sensitive actions have appropriate verification.

### Phase 6 — Administration, Testing, and Release Readiness

- Complete administration pages and permissions.
- Finish relevant reporting and audit trails.
- Test security, performance, database recovery, and external-service failures.
- Test important user journeys from beginning to end.
- Prepare deployment instructions, monitoring, backups, and recovery procedures.

---

## 5. Our Definition of “Completed”

A feature is not complete simply because its page, button, API route, or database table exists.

A feature is considered **Working** only when:

1. The user can perform the intended action.
2. The frontend communicates correctly with the backend.
3. The backend validates and processes the request.
4. The database saves the correct information where required.
5. Errors are handled safely and clearly.
6. Security and permissions are checked.
7. Relevant automated tests pass.
8. The complete user journey has been tested.

Every feature will be classified as:

- **Working:** Tested successfully from beginning to end.
- **Partial:** Some parts work, but the full journey is incomplete.
- **Not Implemented:** The required functionality is missing.
- **Unverified:** The implementation exists, but there is insufficient evidence that it works correctly.

---

## 6. Our Development Rules

1. Protect existing working functionality before making changes.
2. Do not claim a feature works without evidence.
3. Do not replace real functionality with fake success messages or simulated transactions.
4. Clearly label simulated data and demonstration features.
5. Never expose passwords, OTPs, API keys, or unnecessary sensitive personal information in logs.
6. Verify payments with trusted provider records before changing wallet balances.
7. Use database transactions and idempotency for financial operations.
8. Test important failure cases, not only successful cases.
9. Avoid duplicate code and unnecessary architectural complexity.
10. Document major database and architecture decisions, including their reasons and trade-offs.
11. Build for different agricultural activities, not only crop farming.
12. Prioritise reliability, user trust, and practical value over the number of features.

## 7. Final Product Goal

Our goal is to deliver an Agro-Shield platform that farmers can genuinely depend on—not merely a collection of attractive pages.

It should help farmers learn, protect their harvests, understand their markets, connect with buyers, and manage eligible transactions safely.

**Agro-Shield: Protecting Every Harvest. Connecting Every Farmer.**

This is our proposed master plan. The next step is to audit the current repository against it and produce an evidence-based implementation checklist before deciding what to build or fix first.