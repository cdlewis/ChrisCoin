/* The browser only builds GitHub URLs and filters the public balance table. */
const search = document.querySelector('#account-search');
if (search) {
  search.addEventListener('input', () => {
    const query = search.value.trim().toLowerCase();
    let visible = 0;
    document.querySelectorAll('[data-account]').forEach(row => {
      row.hidden = !row.dataset.account.toLowerCase().includes(query);
      if (!row.hidden) visible++;
    });
    const empty = document.querySelector('#no-matches');
    if (empty) empty.hidden = visible !== 0;
  });
}
const form = document.querySelector('#send-form');
if (form) {
  form.addEventListener('submit', event => {
    event.preventDefault();
    const recipient = form.elements.recipient.value.trim();
    const amount = Number(form.elements.amount.value);
    const error = document.querySelector('#send-error');
    if (!recipient || [...recipient].length > 100 || /[\u0000-\u001f\u007f]/.test(recipient) || !Number.isSafeInteger(amount) || amount <= 0) {
      error.textContent = 'Enter a recipient label and a positive whole number of coins.';
      error.hidden = false;
      return;
    }
    const command = { action: 'offer', recipient, amount };
    const url = new URL(`https://github.com/${document.body.dataset.repository}/issues/new`);
    url.search = new URLSearchParams({
      template: 'request.md',
      title: `ChrisCoin: offer ${amount} to ${recipient}`,
      body: '```chriscoin\n' + JSON.stringify(command, null, 2) + '\n```\n\nRecipient labels are decorative. Anyone can accept an open offer.\n'
    }).toString();
    window.location.assign(url.toString());
  });
}
