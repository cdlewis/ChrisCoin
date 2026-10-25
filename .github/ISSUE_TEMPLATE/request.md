---
name: ChrisCoin request
about: Claim an allowance, make an offer, accept, or cancel
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

The recipient label is decorative. **Anyone can accept an open offer.**
An offer reserves its coins until accepted or cancelled by its sender.
Once processing starts, the request is frozen. Submit a new issue to make a correction.
