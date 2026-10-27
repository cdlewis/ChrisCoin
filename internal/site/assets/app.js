/* The browser filters accounts, builds GitHub URLs, and checks snapshot freshness. */
async function refreshSnapshot() {
  const current = document.body.dataset.commit;
  if (!current || document.hidden) return;
  try {
    // A fresh query avoids long-lived intermediary caches on custom Pages domains.
    const response = await fetch(
      `${document.body.dataset.basePath}/data/state.json?fresh=${Date.now()}`,
      { cache: "no-store" },
    );
    if (!response.ok) return;
    const snapshot = await response.json();
    if (!/^[a-f0-9]{40}$/.test(snapshot.commit) || snapshot.commit === current)
      return;
    const next = new URL(window.location.href);
    // Do not loop if different CDN edges briefly disagree during publication.
    if (next.searchParams.get("v") === snapshot.commit) return;
    next.searchParams.set("v", snapshot.commit);
    window.location.replace(next.toString());
  } catch {
    /* The rendered ledger and its publication timestamp remain usable offline. */
  }
}
refreshSnapshot();
setInterval(refreshSnapshot, 60000);
document.addEventListener("visibilitychange", refreshSnapshot);
const search = document.querySelector("#account-search");
if (search) {
  search.addEventListener("input", () => {
    const query = search.value.trim().toLowerCase();
    let visible = 0;
    document.querySelectorAll("[data-account]").forEach((row) => {
      row.hidden = !row.dataset.account.toLowerCase().includes(query);
      if (!row.hidden) visible++;
    });
    const empty = document.querySelector("#no-matches");
    if (empty) empty.hidden = visible !== 0;
  });
}
const form = document.querySelector("#send-form");
if (form) {
  const action = form.elements.action;
  const updateMode = () => {
    const requesting = action.value === "request";
    document.querySelector("#transfer-note").textContent = requesting ? "Confirm your request on GitHub." : "Confirm your transfer on GitHub.";
    document.querySelector("#transfer-title").textContent = requesting
      ? "Request ChrisCoin"
      : "Send ChrisCoin";
    document.querySelector("#recipient-label").textContent = requesting
      ? "Requested from"
      : "Recipient";
    document.querySelector("#transfer-submit").textContent = requesting
      ? "Request on GitHub ↗"
      : "Send on GitHub ↗";
  };
  if (new URL(window.location.href).searchParams.get("mode") === "request")
    action.value = "request";
  action.addEventListener("change", updateMode);
  updateMode();
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    const recipient = form.elements.recipient.value.trim();
    const amount = Number(form.elements.amount.value);
    const error = document.querySelector("#send-error");
    if (
      recipient.length > 20 ||
      !/^[A-Za-z0-9]+(-[A-Za-z0-9]+)*$/.test(recipient) ||
      !Number.isSafeInteger(amount) ||
      amount <= 0
    ) {
      error.textContent =
        "Use a 1–20 character recipient (ASCII letters, digits, single internal hyphens) and a positive whole-number amount.";
      error.hidden = false;
      return;
    }
    const requesting = action.value === "request";
    const command = requesting
      ? { action: "request", payer: recipient, amount }
      : { action: "offer", recipient, amount };
    const url = new URL(
      `https://github.com/${document.body.dataset.repository}/issues/new`,
    );
    url.search = new URLSearchParams({
      template: "request.md",
      title: requesting
        ? `ChrisCoin: request ${amount} from ${recipient}`
        : `ChrisCoin: offer ${amount} to ${recipient}`,
      body: "```chriscoin\n" + JSON.stringify(command, null, 2) + "\n```\n",
    }).toString();
    window.location.assign(url.toString());
  });
}
