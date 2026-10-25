---
name: ChrisCoin request
about: Open an account, make an offer, accept, or cancel
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

Ordinary accounts reserve an offer's coins until acceptance or cancellation.
Recipient names and GitHub usernames must use 1–10 ASCII letters or digits, with single internal hyphens allowed. Profanity is rejected by the backend.
Once processing starts, the request is frozen. Submit a new issue to make a correction.
