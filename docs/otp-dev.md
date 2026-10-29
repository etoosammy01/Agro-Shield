# OTP.dev verification

Agro-Shield uses OTP.dev for phone verification, buyer order payments, wallet
transfers, withdrawals, and password resets. Codes always go to the registered
account phone number, normalized to Nigerian international digits (`234...`).

The API key is the only OTP setting needed in `backend/.env`:

```dotenv
OTPDEV_API_KEY=your_api_key
```

The sender (`OTP Dev`), Agro-Shield template ID
(`a724614a-3a7c-4625-930e-2a8b45141474`), and API URL are built into
`backend/internal/config/config.go`, matching Agrilinks. The template must
contain `{code}` and be available in the account owning the API key.
`OTP_DEV_API_KEY` remains supported as a legacy API key name.

Providing an API key automatically selects `otpdev`. Without a key,
development uses `log`; phone and payment pages display that no SMS was sent
and that the code is in the server log. Production requires an OTP.dev API key.

The backend sends its generated six-digit code using
`POST https://api.otp.dev/v1/verifications`, with the `X-OTP-Key` header and a
`data` object containing `channel`, `sender`, `phone`, `template`, and `code`.
Successful delivery accepts the nested `data.message_id` and `data.phone`
confirmation returned with HTTP 201, as handled by Agrilinks. The flat HTTP 200
confirmation shown in OTP.dev's documentation is also supported. A missing
message ID or mismatched phone rejects the confirmation.

Local bcrypt hashes, account/purpose checks, five-minute expiry, single-use
consumption, five incorrect attempts, and three requests per fifteen minutes
remain in effect. Verification checks the stored code hash locally. Only
delivery calls OTP.dev, so entering a delivered code does not depend on another
API request. A failed delivery invalidates that exact challenge and preserves
a newer resend. Codes and API keys are excluded from provider error messages;
HTTP status and four-digit API error codes remain available for diagnosis.

Selecting **Pay with wallet** opens a confirmation form and sends a payment
code. The wallet is charged only after verification. The same flow protects
`POST /orders/place`. Completed checkout retries read the original buyer's
receipt without issuing another code or charging again.
The confirmed unit price is rechecked before verification and while the crop
is locked for payment; a listing price change cannot charge a different amount.

External checkout clients first request `purpose=payment` at
`POST /wallet/otp/send`, then submit `{"order_id":123,"code":"123456"}` to
`POST /orders/pay`. The order must belong to the logged-in buyer and await
external payment. Flutterwave transaction verification continues to determine
whether money was received; OTP confirms the buyer's action.

Run `go test ./...` from `backend/`. OTP.dev tests use local mock API servers
and do not send real SMS. Repository integration tests require a throwaway
PostgreSQL database in `TEST_DATABASE_URL`.

## Comparison with Agrilinks

Checked against revision
[`a48be2f`](https://github.com/Adewah245/agrilinks/tree/a48be2fc58564b83fe6bc44626b0311276cd516a),
including its
[delivery provider](https://github.com/Adewah245/agrilinks/blob/a48be2fc58564b83fe6bc44626b0311276cd516a/backend/internal/services/otpdev_delivery.go),
[OTP service](https://github.com/Adewah245/agrilinks/blob/a48be2fc58564b83fe6bc44626b0311276cd516a/backend/internal/services/otp_service.go),
and [configuration](https://github.com/Adewah245/agrilinks/blob/a48be2fc58564b83fe6bc44626b0311276cd516a/backend/internal/config/config.go).

| Behavior | Agrilinks | Agro-Shield |
| --- | --- | --- |
| Sender and template | Built-in `OTP Dev` and the Agro-Shield template UUID | Same values |
| SMS request | `POST /v1/verifications`, `X-OTP-Key`, application-generated `data.code` | Same request |
| Successful response | HTTP 201, nested `data` confirmation | Same response; also accepts documented flat HTTP 200 |
| Code verification | Local bcrypt hash, account/purpose checks, single use | Same checks, with atomic phone verification |
| Failed delivery | Deletes the newly issued code | Invalidates the exact hash, retaining the issuance audit and newer resends |
| Validity period | Ten minutes by default, configurable | Five minutes, built in and consistent with the phone/payment forms |
| OTP environment settings | API key required; other options available | Only the API key is read; sender, template, endpoint, and limits are built in |

The provider tests cover the nested HTTP 201 response used by Agrilinks,
local verification without a second provider call, failed-delivery cleanup,
and Nigerian phone formats including `00234...` and `+2340...`.

API reference: [OTP.dev SMS delivery](https://otp.dev/en/docs/sms-otp/).
