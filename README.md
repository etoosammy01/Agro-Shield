# 🌾 Agro-Shield

### **Protecting Every Harvest. Connecting Every Farmer.**

> **Agro-Shield: Empowering Farmers, Protecting Every Harvest.**

Agro-Shield is a digital agriculture platform designed to empower farmers, connect them with buyers, improve access to agricultural information, and reduce post-harvest losses.

The platform provides farmers and agricultural stakeholders with tools for **produce management, storage management, marketplace access, AI-powered agricultural assistance, market-price insights, communication, negotiations, payments, deliveries, and more**.

Agro-Shield is being developed with a focus on communities where farmers may have limited access to modern digital services, while maintaining an architecture that can scale beyond the Idoma community.

---

## 📌 Table of Contents

* [Problem](#-problem)
* [Our Solution](#-our-solution)
* [Key Features](#-key-features)
* [User Roles](#-user-roles)
* [Farmer Features](#-farmer-features)
* [Buyer Features](#-buyer-features)
* [AI Agricultural Assistant](#-ai-agricultural-assistant)
* [Marketplace](#-marketplace)
* [Storage & Produce Management](#-storage--produce-management)
* [Market Price Insights](#-market-price-insights)
* [Chat & Negotiations](#-chat--negotiations)
* [Orders, Payments & Deliveries](#-orders-payments--deliveries)
* [Notifications](#-notifications)
* [USSD Access](#-ussd-access)
* [Profile & Account Management](#-profile--account-management)
* [Authentication & Security](#-authentication--security)
* [Technology Stack](#-technology-stack)
* [Project Architecture](#-project-architecture)
* [Project Structure](#-project-structure)
* [Database](#-database)
* [Getting Started](#-getting-started)
* [Environment Variables](#-environment-variables)
* [Development](#-development)
* [Testing](#-testing)
* [Project Status](#-project-status)
* [Future Improvements](#-future-improvements)
* [Team](#-team)
* [License](#-license)

---

# 🌍 Problem

Agriculture is one of the most important sectors supporting communities across Nigeria, but farmers continue to face major challenges such as:

* Limited access to reliable buyers
* Poor market visibility
* Post-harvest losses
* Difficulty managing stored produce
* Limited access to agricultural information
* Difficulty determining fair market prices
* Lack of direct communication between farmers and buyers
* Transportation and delivery challenges
* Limited access to digital financial services
* Limited connectivity and access to smartphones in some rural communities

These challenges can reduce farmers' income and make it difficult for buyers to reliably source agricultural products.

---

# 💡 Our Solution

Agro-Shield provides a centralized digital platform where farmers and buyers can interact throughout the agricultural transaction lifecycle.

### Agro-Shield connects:

**Farmers → Produce → Storage → Marketplace → Buyers → Negotiation → Payment → Delivery**

The platform also provides agricultural intelligence through AI assistance and market-price information.

Our goal is to make agricultural commerce more accessible, transparent, and efficient.

---

# 🚀 Key Features

Agro-Shield currently provides or supports:

* 👨‍🌾 Farmer registration
* 🛒 Buyer registration
* 🔐 Secure authentication
* 👤 Farmer and buyer profiles
* 📸 Profile picture upload and identification
* 🏦 Farmer bank/account details
* 📦 Produce and crop management
* 🏠 Storage management
* 🛍️ Agricultural marketplace
* 🛒 Shopping cart
* 💰 Sales management
* 📈 Market-price insights
* 🤖 AI agricultural assistant
* 🌱 AI crop-diagnosis support
* 🧾 AI diagnosis history
* 💬 User-to-user chat
* 🤝 Buyer/farmer negotiations
* 📋 Order management
* 💳 Payment integration
* 🚚 Delivery management
* 🔔 Notifications
* 📱 USSD access
* 🌦️ Weather-related agricultural assistance
* 📷 Image processing and upload support

---

# 👥 User Roles

Agro-Shield currently supports two primary user roles:

### 👨‍🌾 Farmer

Farmers can:

* Create an account
* Complete their profile
* Upload a profile picture
* Add account/payment details
* Manage crops and produce
* Manage stored produce
* List products for sale
* View marketplace activity
* Communicate with buyers
* Negotiate prices
* Receive orders
* Manage sales
* Track deliveries
* Access AI agricultural assistance
* View agricultural market information
* Receive notifications

### 🛒 Buyer

Buyers can:

* Create an account
* Complete their profile
* Upload a profile picture
* Browse agricultural products
* Add products to a cart
* Place orders
* Communicate with farmers
* Negotiate where supported
* Make payments
* Track deliveries
* View purchase history
* Manage their profile

---

# 👨‍🌾 Farmer Features

## Registration

Farmers can create accounts by providing information such as:

* First name
* Last name
* Phone number
* Email
* Password
* Location
* Account role

The system validates required information and prevents duplicate phone-number registration.

Passwords are securely hashed before being stored.

---

## Complete Your Profile

After registration, users are automatically taken through a mandatory profile-completion process.

Farmers can:

* Upload a profile picture
* Provide bank name
* Provide account name
* Provide account number

The profile picture helps identify users on the platform.

Bank details provide the foundation for future and existing payment-related functionality.

---

# 🛒 Buyer Features

Buyers can interact with the agricultural marketplace to:

* Browse available produce
* View product information
* Add products to a shopping cart
* Place orders
* Communicate with sellers
* Negotiate with farmers where applicable
* Make payments
* Track purchases and deliveries
* View purchase history

---

# 🤖 AI Agricultural Assistant

Agro-Shield includes an AI-powered agricultural assistant designed to provide farmers with accessible agricultural guidance.

The assistant can help users obtain information relating to agricultural activities and farming decisions.

The system is designed around AI services and providers, with support for agricultural assistance and external AI APIs.

### Planned/expanding AI capabilities include:

* Crop disease assistance
* Crop image analysis
* Treatment recommendations
* Disease prevention guidance
* Farming recommendations
* Weather-informed agricultural assistance

---

# 🌱 AI Crop Diagnosis

Agro-Shield includes support for AI-assisted crop diagnosis.

The intended workflow is:

```text
Farmer
   │
   ▼
Upload Crop Image
   │
   ▼
AI Analysis
   │
   ▼
Possible Disease / Condition
   │
   ▼
Treatment Recommendation
   │
   ▼
Prevention Guidance
```

Diagnosis information can also be stored so that users can access their previous AI diagnosis history.

---

# 🛍️ Marketplace

The marketplace connects farmers directly with potential buyers.

Farmers can:

* Create product listings
* Provide product information
* Manage available produce
* Manage sales

Buyers can:

* Browse available products
* View product details
* Add products to their cart
* Place orders

This creates a direct digital connection between producers and buyers.

---

# 📦 Storage & Produce Management

Farmers can manage agricultural produce through the storage functionality.

The storage system is intended to help farmers keep track of:

* Stored produce
* Crop information
* Quantities
* Storage-related records

The goal is to improve visibility over stored agricultural products and reduce avoidable post-harvest losses.

---

# 📈 Market Price Insights

Agro-Shield includes market-price functionality designed to provide useful information about agricultural prices.

The system includes a dedicated market-price architecture containing:

* Price information
* Market-price services
* Price repositories
* Market-price API handlers
* Administrative price functionality
* Market statistics

This functionality helps provide farmers with better information when making decisions about selling their produce.

---

# 💬 Chat & Negotiations

Agro-Shield provides communication functionality between platform users.

### Chat

Users can communicate with one another through the platform.

The backend supports:

* Conversations
* Conversation members
* Chat messages

### Negotiations

The platform also supports negotiation between buyers and farmers.

This provides a foundation for direct discussion around agricultural transactions and pricing.

---

# 📋 Orders, Payments & Deliveries

Agro-Shield supports the transaction lifecycle from product selection to delivery.

### Order flow

```text
Product
   ↓
Cart
   ↓
Order
   ↓
Payment
   ↓
Delivery
```

The backend contains dedicated functionality for:

* Orders
* Payments
* Payment processing
* Payment verification
* Sales
* Deliveries

---

## 💳 Payments

Agro-Shield integrates with **Flutterwave** for payment processing.

The payment system supports functionality including:

* Payment initialization
* Hosted checkout
* Transaction verification
* Payment references
* Payment status tracking
* Webhook verification
* Payment records

Payment statuses include:

* `pending`
* `successful`
* `failed`

Payment processing is designed so that transaction status is verified rather than relying solely on information supplied by a client-side redirect or webhook.

---

# 🚚 Deliveries

The delivery functionality provides a foundation for managing agricultural orders after purchase.

It is intended to support the movement of produce from sellers to buyers and provide better visibility throughout the fulfillment process.

---

# 🔔 Notifications

Agro-Shield includes a notification system to keep users informed about relevant activities.

Notifications can support events related to:

* Orders
* Payments
* Sales
* Messages
* Negotiations
* Deliveries
* Other important platform activities

---

# 📱 USSD Access

Agro-Shield includes a USSD access layer to support users who may have limited access to smartphones or reliable internet connectivity.

The USSD system provides access to selected agricultural platform functionality through basic mobile phones.

The current USSD functionality includes access to areas such as:

* Storage
* Marketplace
* Sales
* Notifications
* Profile

The broader goal is to make Agro-Shield accessible beyond smartphone-only users.

---

# 👤 Profile & Account Management

Users can manage their personal information through the platform.

Profile functionality includes:

* Full name
* Phone number
* Email
* Location
* Profile picture

Users can also update their profile information after registration.

### Profile workflow

```text
Registration
     ↓
Complete Profile
     ↓
Profile Picture
     ↓
Account Details
     ↓
Dashboard
     ↓
Profile Management
```

The profile system also supports viewing relevant user information and role-specific activity.

For example:

* Farmers can view their crops and sales.
* Buyers can view their purchases.
* Users can access their own profile information.
* Public profile information can be displayed where appropriate.

---

# 🔐 Authentication & Security

Agro-Shield includes several security mechanisms.

### Password security

User passwords are not stored as plain text.

Passwords are securely hashed before being persisted.

### Session authentication

After successful authentication, the platform creates a session for the user.

Authenticated requests can then identify the current farmer/buyer through the session.

### Profile completion protection

New accounts are required to complete the mandatory profile-picture step before accessing protected areas of the application.

### Payment security

Payment functionality includes:

* Transaction verification
* Webhook signature verification
* Secure server-side communication with Flutterwave
* Payment status validation

### Input validation

Registration and other request handlers validate incoming user data before processing it.

---

# 🛠️ Technology Stack

## Backend

* **Go**
* `net/http`
* PostgreSQL
* `pgx`
* Go HTML templates
* `golang.org/x/crypto`
* Image processing libraries
* Google Gemini / GenAI integration
* WebSocket support where required

## Frontend

* HTML
* CSS
* JavaScript
* Go HTML templates

## Database

* **PostgreSQL**

SQLite was used during the early MVP stage, but PostgreSQL is now the target/current database architecture.

## Payment

* Flutterwave

## Version Control

* Git
* GitHub

---

# 🏗️ Project Architecture

Agro-Shield follows a layered backend architecture.

```text
                    ┌─────────────────────┐
                    │      FRONTEND       │
                    │ HTML / CSS / JS     │
                    └──────────┬──────────┘
                               │
                               ▼
                    ┌─────────────────────┐
                    │     HTTP SERVER     │
                    │      Go/net/http    │
                    └──────────┬──────────┘
                               │
                               ▼
                    ┌─────────────────────┐
                    │      HANDLERS       │
                    │ HTTP request logic   │
                    └──────────┬──────────┘
                               │
                               ▼
                    ┌─────────────────────┐
                    │      SERVICES       │
                    │ Business logic       │
                    └──────────┬──────────┘
                               │
                               ▼
                    ┌─────────────────────┐
                    │    REPOSITORIES     │
                    │ Database operations  │
                    └──────────┬──────────┘
                               │
                               ▼
                    ┌─────────────────────┐
                    │     PostgreSQL      │
                    │      Database       │
                    └─────────────────────┘
```

### General request flow

```text
Browser
   ↓
Route
   ↓
Middleware
   ↓
Handler
   ↓
Service
   ↓
Repository
   ↓
PostgreSQL
```

This separation helps keep HTTP handling, business logic, and database operations organized.

---

# 📁 Project Structure

A simplified view of the backend:

```text
backend/
│
├── cmd/
│   └── market-import/
│       └── main.go
│
├── handlers/
│   ├── ai-assistant.go
│   ├── ai-diagnosis-history.go
│   ├── cart.go
│   ├── chat_handler.go
│   ├── complete_profile.go
│   ├── dashboard.go
│   ├── delivery.go
│   ├── forgot_password.go
│   ├── index.go
│   ├── login.go
│   ├── logout.go
│   ├── market.go
│   ├── negotiation.go
│   ├── notification_handler.go
│   ├── order.go
│   ├── payment_handler.go
│   ├── product.go
│   ├── profile.go
│   ├── profile_edit.go
│   ├── register.go
│   ├── sales.go
│   ├── storage.go
│   └── uploads.go
│
├── internal/
│   ├── app/
│   ├── config/
│   ├── database/
│   ├── dto/
│   ├── models/
│   ├── repository/
│   └── services/
│
├── middleware/
│   ├── auth.go
│   ├── onlyget.go
│   └── onlypath.go
│
├── migrations/
│
├── payments/
│
├── render/
│   └── render.go
│
├── routes/
│   └── Routes.go
│
├── ussd/
│   └── handler.go
│
├── main.go
├── go.mod
└── go.sum
```

> Some development/testing components may exist separately from the main application architecture. They should not automatically be considered part of the production request flow.

---

# 🗄️ Database

Agro-Shield uses **PostgreSQL** as its database.

The database layer is separated from application business logic through repositories.

Major data areas include:

* Farmers/users
* Crops
* Products
* Storage
* Orders
* Payments
* Deliveries
* Notifications
* Conversations
* Conversation members
* Chat messages
* Negotiations
* Diagnosis history
* Market events
* Other agricultural records

### Database architecture

```text
Application
     │
     ▼
Services
     │
     ▼
Repositories
     │
     ▼
PostgreSQL
```

Database schema changes are managed through migration files.

---

# ⚙️ Getting Started

## 1. Clone the repository

```bash
git clone <repository-url>
cd Agro-Shield
```

## 2. Enter the backend

```bash
cd backend
```

## 3. Install dependencies

```bash
go mod download
```

or:

```bash
go mod tidy
```

## 4. Configure environment variables

Create a `.env` file containing the required database, authentication, AI, payment, and application configuration.

Example:

```env
DATABASE_URL=your_postgresql_connection_string

FLUTTERWAVE_SECRET_KEY=your_flutterwave_secret_key
FLUTTERWAVE_SECRET_HASH=your_flutterwave_secret_hash

GEMINI_API_KEY=your_gemini_api_key

APP_URL=http://localhost:8080
```

> Never commit real API keys, passwords, database credentials, or other secrets to GitHub.

## 5. Run the application

```bash
go run .
```

The application will normally be available at:

```text
http://localhost:8080
```

---

# 🔑 Environment Variables

Depending on the enabled features, Agro-Shield may require environment variables for:

| Variable                  | Purpose                        |
| ------------------------- | ------------------------------ |
| `DATABASE_URL`            | PostgreSQL connection          |
| `FLUTTERWAVE_SECRET_KEY`  | Flutterwave API authentication |
| `FLUTTERWAVE_SECRET_HASH` | Webhook verification           |
| `GEMINI_API_KEY`          | AI services                    |
| `APP_URL`                 | Application/base URL           |

Actual environment variables should be documented alongside the relevant feature as the system evolves.

---

# 🧪 Testing

Agro-Shield includes automated tests for selected parts of the application.

Testing currently covers areas including:

* Dashboard functionality
* Market-price services
* Market event functionality
* Repository functionality
* AI provider functionality
* USSD handlers
* Payment-related components where applicable

Run all Go tests with:

```bash
go test ./...
```

Run tests with additional output:

```bash
go test -v ./...
```

Check all project packages with:

```bash
go list ./...
```

---

# 📊 Project Status

### Current implementation

Agro-Shield has progressed beyond the initial MVP and now includes multiple interconnected platform components.

### Completed / implemented areas include:

* [x] User registration
* [x] Farmer accounts
* [x] Buyer accounts
* [x] Authentication
* [x] Session management
* [x] Farmer profile management
* [x] Buyer profile management
* [x] Profile picture upload
* [x] Mandatory profile completion
* [x] Farmer bank/account details
* [x] PostgreSQL database architecture
* [x] Crop management
* [x] Product management
* [x] Marketplace
* [x] Shopping cart
* [x] Sales management
* [x] Storage management
* [x] Orders
* [x] Payments
* [x] Flutterwave integration
* [x] Payment verification
* [x] Deliveries
* [x] Notifications
* [x] Chat/conversations
* [x] Negotiations
* [x] AI agricultural assistant
* [x] AI diagnosis history
* [x] Market-price functionality
* [x] USSD in progress
* [x] Automated tests for selected components

---

# 🔮 Future Improvements

Agro-Shield is continuously evolving.

Potential future improvements include:

### 🤖 Advanced AI

* More accurate crop disease detection
* More crop types
* Localized treatment recommendations
* Personalized farming recommendations
* AI-powered yield prediction
* AI-powered price prediction
* Voice-based agricultural assistance

### 📱 Rural accessibility

* Expanded USSD functionality
* SMS notifications
* Low-bandwidth optimization
* Offline-first capabilities
* Local-language support

### 🛒 Marketplace

* Improved product discovery
* Advanced search and filtering
* Farmer verification
* Buyer verification
* Ratings and reviews
* Improved market analytics

### 🚚 Logistics

* Improved delivery tracking
* Logistics-provider integration
* Route optimization
* Delivery status notifications

### 💰 Financial services

* Improved payment workflows
* Transaction history
* Farmer payout management
* More payment providers
* Financial reporting

### 📊 Agricultural intelligence

* More market-price sources
* Price trend analysis
* Regional agricultural insights
* Crop demand forecasting
* Agricultural data analytics

---

# 🌱 Vision

Agro-Shield is being built with a larger vision:

> **To create a digital agricultural ecosystem that helps farmers protect their harvest, access better markets, make informed decisions, and improve their income.**

The platform is designed not only as a marketplace, but as a broader agricultural technology ecosystem connecting:

```text
             FARMER
                │
       ┌────────┼────────┐
       │        │        │
       ▼        ▼        ▼
    STORAGE   AI      MARKET
       │        │        │
       └────────┼────────┘
                │
                ▼
            MARKETPLACE
                │
                ▼
             BUYER
                │
          ┌─────┴─────┐
          ▼           ▼
       PAYMENT      DELIVERY
          │           │
          └─────┬─────┘
                ▼
          SUCCESSFUL
          TRANSACTION
```

---

# 🤝 Team

**Team Agro-Shield**

Built for the **Idoma Centenary Plus Hackathon 2026**.

### Track

**Agro-Innovation**

### Project

**Agro-Shield**

### Tagline

> **Protecting Every Harvest. Connecting Every Farmer.**

---

# 📜 License

Agro-Shield is distributed under the project's license and team agreement.

See:

```text
LICENSE
```

and:

```text
TEAM_AGREEMENT.md
```

for the applicable ownership, usage, contribution, and intellectual-property terms.

---

# ⭐ Acknowledgements

Agro-Shield was developed as part of the **Idoma Centenary Plus Hackathon 2026**, with the goal of applying technology to real agricultural challenges affecting farmers and communities.

---

## 🌾 Agro-Shield

### **Protecting Every Harvest. Connecting Every Farmer.**

**Empowering Farmers. Protecting Every Harvest.**
