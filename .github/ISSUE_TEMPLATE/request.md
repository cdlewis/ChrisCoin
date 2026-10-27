---
name: ChrisCoin request
about: Open an account, send coins, request payment, or settle a request
title: 'ChrisCoin: request'
---

Your signed-in GitHub account authorizes this request. Replace the JSON below with one command.

```chriscoin
{"action":"claim"}
```

Other commands (put only one in the block above):

- Offer: `{"action":"offer","recipient":"Amanda","amount":10}`
- Accept: `{"action":"accept","offerId":"offer-123"}`
- Cancel: `{"action":"cancel","offerId":"offer-123"}`

- Request payment: `{"action":"request","payer":"Amanda","amount":25}`
- Pay request: `{"action":"fulfill","requestId":"request-123"}`
- Cancel request: `{"action":"cancel-request","requestId":"request-123"}`

Payments to existing ledger usernames settle immediately. For new recipients, ordinary accounts reserve an offer's coins until acceptance or cancellation.
Recipient names and GitHub usernames must use 1–20 ASCII letters or digits, with single internal hyphens allowed. Profanity is rejected by the backend.
Once processing starts, the request is frozen. Submit a new issue to make a correction.
