# Agro-Shield Flutterwave Sandbox Setup

This guide covers the current wallet deposit/withdrawal integration and the protected marketplace payment endpoint.

## 1. Requirements

- Go 1.25.0 (the version requested by `backend/go.mod`)
- PostgreSQL, with a database created for Agro-Shield
- A Gemini API key and either a Groq or OpenRouter API key (the application currently requires these at startup)
- Flutterwave test credentials from the Flutterwave dashboard

## 2. Configure environment

From the repository root:

```bash
cp backend/.env.example backend/.env
```

Edit `backend/.env` and replace all placeholders. Keep the Flutterwave key in test mode (`FLWSECK_TEST-...`) while testing. Set `WALLET_ADMIN_USER_IDS` to the numeric ID of your own test admin account in the database. Do not commit `backend/.env` or share its contents.

The `PAYMENT_REDIRECT_URL` can be `http://localhost:8080/payments/callback` for the browser return during local testing. Flutterwave cannot send server-to-server webhooks to localhost. To test webhooks, use a trusted HTTPS tunnel or deployed test server and configure that public URL in the Flutterwave test dashboard.

## 3. Start the backend

```bash
cd backend
go version
go mod download
go test ./...
go run .
```

The `go version` output should show Go 1.25.x. The application runs database migrations at startup. If startup reports a missing environment variable or database connection error, fix that before testing payments.

## 4. Test a wallet deposit

1. Sign in as a test farmer.
2. Open `/wallet` and submit a small deposit amount.
3. Confirm that the browser opens Flutterwave's hosted checkout page.
4. Complete the payment using Flutterwave's current test payment details.
5. Return to Agro-Shield and check the wallet balance and deposit history.
6. Confirm that a successful verified deposit credits the wallet once; a failed/cancelled attempt must not credit it.
7. If the payment remains pending, do not submit another deposit immediately. Check the deposit history and webhook delivery first.

## 5. Test a withdrawal

1. Complete a successful sandbox deposit first.
2. Save valid test bank details on the wallet page.
3. Request a withdrawal within the available balance.
4. Sign in as the user ID listed in `WALLET_ADMIN_USER_IDS` and open `/admin/wallet-withdrawals`.
5. Approve the test request only if your Flutterwave test account supports test transfers. Verify the resulting transfer status before considering it complete.
6. If the transfer request times out or is pending, use the reconciliation action rather than repeatedly approving it.

## 6. Webhook configuration

Configure the Flutterwave TEST webhook URL as:

```text
https://YOUR-PUBLIC-HTTPS-HOST/webhooks/flutterwave
```

Set the webhook secret hash to exactly the value in `FLUTTERWAVE_SECRET_HASH`. The webhook handler checks the `verif-hash` header and re-verifies payment/transfer details with Flutterwave before changing wallet state.

## 7. Marketplace payment status

The `/orders/pay` endpoint now accepts only an `order_id`, identifies the buyer from the authenticated session, loads the amount and buyer details from PostgreSQL, and rejects orders that do not belong to the buyer or are not pending external payment. It marks eligible orders as paid only after verification.

**Current limitation:** the existing marketplace's regular order form still uses the internal Agro-Shield wallet-payment flow. It does not yet create `pending_payment` orders or redirect to Flutterwave checkout. Therefore, wallet deposit/withdrawal sandbox testing is the immediately testable flow. Do not assume marketplace external checkout is end-to-end until its order-creation UI is explicitly connected to this endpoint.

## Security reminders

- Never use live keys during sandbox testing.
- Never put the Flutterwave secret key in frontend code.
- Never treat a browser redirect alone as proof of payment.
- Verify transaction ID, reference, status, amount and currency server-side.
- Test duplicate webhooks and failed/cancelled payments before enabling live transactions.
