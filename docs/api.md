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
- `POST /wallet/withdraw` accepts a whole-naira `amount`. The amount is reserved immediately; it is paid after admin approval or returned if rejected or the transfer fails.
- `GET /admin/wallet-withdrawals` and its `POST /approve` and `/reject` actions are restricted to user IDs in `WALLET_ADMIN_USER_IDS`.

Flutterwave sends payment and transfer callbacks to `POST /webhooks/flutterwave`. Both are signature-checked; deposits and payouts are re-verified directly with Flutterwave before wallet balances or withdrawal status are changed.

---

## GET /dashboard

Farmer dashboard.