# API Documentation

## GET /

Returns homepage.

---

## GET /register

Registration page. Nigerian state and LGA options are loaded from the
location-directory endpoints; community is entered freely.

---

## POST /register

Registers an account. The submitted locality, state/province/region, and
country are stored with the account.

Form fields

```text
first-name, last-name, phone, email, password, confirm-password,
role, community, lga, state, country
```

The saved profile shows community, LGA, state, and country separately.

## GET /locations/nigeria/states

Returns the current Nigerian state list as a JSON array. The response is
cached by the backend and browser for up to 24 hours.

## GET /locations/nigeria/lgas?state={state}

Returns a JSON array of local government areas for the requested state.

## POST /learning/chat

Accepts the selected `farmingType` together with the chat `messages`. The AI
uses that farming category to focus its lesson.

```json
{
  "farmingType": "Poultry farming",
  "messages": [{"role": "user", "content": "How should I prepare a coop?"}]
}
```

---

## GET /login

Login page.

---

## POST /login

Authenticates user.

---

## Wallet

- `GET /wallet` displays the authenticated user's NGN balance, deposit and withdrawal activity, and payout-bank details.
- `POST /wallet/deposit` accepts `amount` and starts a Flutterwave hosted checkout. The wallet is credited only after the transaction is verified.
- `POST /wallet/bank` accepts `bank_name`, `bank_code`, `account_name`, and a 10-digit `account_number`.
- `POST /wallet/otp/send` accepts `purpose=withdrawal`, `transfer`, or `payment` and sends a code to the authenticated account's phone. Order payment codes require a buyer account. The response contains a `message` or `error`; rate limits return HTTP 429.
- `POST /wallet/withdraw` accepts a whole-naira `amount` and a six-digit `code`. After code verification the amount is reserved; it is paid after admin approval or returned if rejected or the transfer fails.
- `POST /wallet/transfer` accepts `recipient`, `amount`, `note`, and a six-digit `code`. The transfer is submitted for admin approval after verification.
- `GET /admin/wallet-withdrawals` and its `POST /approve` and `/reject` actions are restricted to user IDs in `WALLET_ADMIN_USER_IDS`.

Flutterwave sends payment and transfer callbacks to `POST /webhooks/flutterwave`. Both are signature-checked; deposits and payouts are re-verified directly with Flutterwave before wallet balances or withdrawal status are changed.

## Phone verification

`GET /verify-phone` displays the onboarding verification step after the selfie
has been captured. `POST /verify-phone` accepts `action=send_code`, or
`action=verify_code` and `code`. Successful verification ends the onboarding
session and redirects to login.

## Order payment verification

The marketplace buy form and `POST /orders/place` accept `crop_id`, `quantity`,
and `checkout_token`. The first submission sends a payment code and displays
the confirmation form. Submit `action=verify_payment`, `code`, and the
confirmation form's `unit_price` with those same fields to pay. A changed
price requires review before payment. `action=send_payment_code` resends the code. A completed
checkout token returns its buyer's original receipt without charging again.

`POST /orders/pay` starts external checkout for a pending order. Request a
payment code through `/wallet/otp/send`, then send:

```json
{"order_id":123,"code":"123456"}
```

Only the logged-in buyer may pay, and a valid payment-purpose code is required.
The amount and customer details are loaded from the database. The success
response contains `checkout_url` and `reference`. Flutterwave verifies payment
settlement independently of the OTP.

See [OTP.dev setup](otp-dev.md) for environment configuration.

---

## GET /dashboard

Farmer dashboard.
