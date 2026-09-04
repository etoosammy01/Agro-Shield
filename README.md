# 🌾 Agro-Shield

> **Protecting Every Harvest. Connecting Every Farmer.**

Agro-Shield is a digital agriculture platform designed to reduce post-harvest losses, improve market access, and empower farmers across Idoma communities and beyond.

Built for the **Idoma Centenary Plus Hackathon 2026**, the platform addresses one of Benue State's biggest agricultural challenges: helping farmers store, manage, and sell their produce at fair market prices while providing access to intelligent farming assistance.

---

# 📌 Problem Statement

Benue State is one of Nigeria's largest producers of agricultural products, yet many farmers continue to experience:

- High post-harvest losses
- Poor access to verified buyers
- Unstable market prices
- Limited storage management
- Lack of modern digital farming tools
- Poor access to agricultural information

Many smallholder farmers are forced to sell immediately after harvest at very low prices because they lack information and access to larger markets.

**Agro-Shield aims to bridge this gap.**

---

# 💡 Our Solution

Agro-Shield provides farmers with one platform where they can:

- Register as farmers or buyers
- Manage stored produce
- Connect directly with buyers
- Access an online agricultural marketplace
- Receive AI-powered farming assistance
- Track farming activities through a personalized dashboard

The platform is designed with rural accessibility and future low-connectivity support in mind.

---

# 🚀 Current Features

## ✅ Landing Page

- Modern responsive homepage
- Project introduction
- Easy navigation
- Farmer-focused branding

---

## ✅ User Registration

Farmers can register by providing:

- Full Name
- Phone Number
- Password
- Location

Backend validation includes:

- Required-field validation
- Duplicate phone-number detection
- Password hashing using bcrypt
- Persistent database storage

---

## ✅ Login Page

Secure login interface with:

- Phone number authentication
- Password verification
- Responsive design

---

## ✅ Farmer Dashboard

The dashboard currently includes:

- Welcome screen
- Product overview
- Marketplace overview
- Revenue overview
- AI diagnoses
- Navigation system
- Quick-action cards including storage, marketplace, and AI assistant

### 🤖 AI Farming Assistant

The AI assistant is currently available as part of the Agro-Shield platform.

It provides farmers with AI-powered agricultural assistance, including:

- Answers to crop-related questions
- Agricultural guidance
- Crop disease assistance
- Treatment recommendations
- Preventive farming advice

The AI assistant will continue to be improved with more localized agricultural knowledge, crop-specific recommendations, and image-based disease detection.

---

## ✅ Buyer Dashboard

Buyers have a dedicated dashboard for:

- Marketplace access
- Purchase history
- Profile management

---

# 🗄️ Database Architecture

Agro-Shield initially used **SQLite** during MVP development because it was lightweight and suitable for rapid development and local testing.

The project has now been successfully migrated to **PostgreSQL** to provide a stronger foundation for production deployment, multi-user access, and future marketplace transactions.

### Why PostgreSQL?

PostgreSQL provides Agro-Shield with:

- Better support for concurrent users
- Strong transactional capabilities
- Improved data integrity
- Better scalability
- Production and cloud deployment readiness
- A stronger foundation for marketplace transactions
- Support for larger farmer, buyer, and transaction datasets

### Database Evolution

#### Initial MVP

```text
Go Application
      │
      ▼
Repository Layer
      │
      ▼
SQLite
```

### Current Production Architecture

```text
Frontend
    │
    ▼
Go HTTP Server
    │
    ▼
Handlers
    │
    ▼
Service Layer
    │
    ▼
Repository Layer
    │
    ▼
PostgreSQL
```

The repository pattern allows the database layer to evolve without requiring major changes to the application's business logic.

### PostgreSQL Migration Plan

The migration is being implemented in stages:

1. Set up the PostgreSQL development database.
2. Create PostgreSQL-compatible migrations.
3. Update the Go database connection layer.
4. Replace SQLite-specific database configuration and queries where required.
5. Update repositories and data-access components.
6. Test registration and authentication flows.
7. Test marketplace and future transaction workflows.
8. Configure production database deployment.
9. Remove the SQLite dependency after PostgreSQL integration has been fully validated.

> **Current Status:** PostgreSQL migration is complete. SQLite remains part of the project's initial MVP development history.

---

# ✅ Backend Architecture

Agro-Shield follows a layered backend architecture designed to separate responsibilities, improve maintainability, and support future scalability.

```text
Handlers
    ↓
Services
    ↓
Repositories
    ↓
Database Layer
    ↓
PostgreSQL
```

Current backend components include:

* HTTP routing
* Middleware
* Handler layer
* Service layer
* Repository pattern
* Data Transfer Objects (DTOs)
* Domain models
* PostgreSQL database
* Database migrations
* Authentication and authorization
* HTML template rendering
* Static asset serving
* AI-powered agricultural assistance
* Marketplace functionality
* Order and negotiation management
* Cart management
* Notifications
* Conversations and messaging

For historical details about the database migration, see Database Migration (docs/database-migration.md).

---

# 🛠 Technology Stack

### Backend

- Go (Golang)
- `net/http`
- PostgreSQL
- bcrypt
- Go HTML Templates
- Google Gemini API

### Previous MVP Technology

- SQLite

### Frontend

* HTML5
* CSS3
* JavaScript

### Development & Version Control

* Git
* GitHub

---

# 📂 Project Structure

```text
Agro-Shield/
│
├── backend/
│   ├── handlers/
│   ├── internal/
│   │   ├── database/
│   │   ├── dto/
│   │   ├── models/
│   │   ├── repository/
│   │   └── services/
│   │
│   ├── middleware/
│   ├── migrations/
│   ├── render/
│   ├── routes/
│   ├── main.go
│   ├── go.mod
│   └── go.sum
│
├── frontend/
│   ├── assets
│   ├── css
│   ├── js
│   └── pages
│
├── docs/
│
├── README.md
├── CONTRIBUTING.md
└── LICENSE
```

---

# 🌍 Target Users

* Smallholder Farmers
* Commercial Farmers
* Agricultural Cooperatives
* Produce Buyers
* Agricultural Extension Workers

---

# 🎯 Hackathon Objectives

Our solution focuses on:

* Reducing post-harvest losses
* Improving market accessibility
* Increasing farmer income
* Supporting digital agriculture
* Creating a scalable platform for rural communities
* Expanding access to agricultural information

---

# 🔮 Future Roadmap

The current version includes the core platform foundation, user authentication, farmer and buyer dashboards, and working AI-powered agricultural assistance.

Future releases will introduce the following features.

---

## 🌾 Smart Marketplace

* Direct farmer-to-buyer trading
* Verified buyer accounts
* Live product listings
* Secure transaction tracking

---

## 📈 AI Market Price Advisor

An intelligent recommendation system that helps farmers decide:

* When to sell
* Where to sell
* Expected market trends
* Price forecasting

Example:

```text
Today's Yam Prices

Otukpo
₦52,000 / ton

Makurdi
₦58,000 / ton

Recommendation

Wait 2 days before selling.
Expected increase: +8%
```

---

## 📦 Smart Produce Storage

Future versions will allow farmers to:

* Track stored produce
* Monitor storage duration
* Receive spoilage alerts
* Monitor warehouse conditions
* Integrate IoT sensors for storage environments

---

## 📱 USSD Support

To improve accessibility for rural communities:

* Register without internet
* Check market prices
* Receive farming tips
* Access emergency alerts

This will allow farmers using basic feature phones to benefit from Agro-Shield without requiring a smartphone.

---

## 🌐 Multi-language Support

Planned support includes:

* English
* Idoma
* Igede
* Apa
* Ufia
* Other relevant local languages

---

## 👨‍🌾 Cooperative Management

Farmers will be able to:

* Form cooperatives
* Aggregate produce
* Negotiate bulk sales
* Share transportation
* Manage cooperative activities digitally

---

## 🚚 Logistics Integration

Future releases will include:

* Transport booking
* Produce delivery tracking
* Warehouse locations
* Buyer pickup scheduling

---

## 📊 Farmer Analytics

Dashboard insights including:

* Sales history
* Revenue trends
* Produce statistics
* Storage reports
* Market trends

---

# 🏆 Innovation

Agro-Shield combines:

* Digital Marketplace
* Smart Storage Management
* AI Farming Assistant
* Future Price Prediction
* Farmer-Centered Design
* Low-connectivity accessibility

to create an integrated agricultural ecosystem.

---

# 🌱 Sustainability

The platform is designed to:

* Increase farmer income
* Reduce food waste
* Improve food security
* Encourage digital inclusion
* Strengthen rural economies
* Support scalable agricultural development

---

# 👥 Team

Developed by **Team Agro-Shield**

Built for the **Idoma Centenary Plus Hackathon 2026**.

Together, we believe technology can transform agriculture and empower every farmer.

Agro-Shield is built by a multidisciplinary team working together to develop
technology that empowers farmers and reduces post-harvest losses.

| Team Member | Role |
|---|---|
| Akilozi Samuel | Team Lead |
| Otete Benjamin Agogo | Frontend Engineer |
| Adewa James | Backend Engineer |
| Emmanuel Elaigwu | Backend Engineer |
| Agene Okoh | Frontend Engineer |
| Alex Ugwu | Backend Engineer |
| Ogaba Gabriel Eko | Backend Engineer |


For detailed team profiles, see [`docs/team/`](docs/team/).
---

# 📖 Vision

> **To become the leading digital agriculture platform connecting every farmer to better opportunities while reducing post-harvest losses through technology and innovation.**

---

# ❤️ Motto

**Protect Every Harvest. Empower Every Farmer.**
