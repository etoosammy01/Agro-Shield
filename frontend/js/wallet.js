const bankSearch = document.getElementById("bank-search");
const bankCode = document.getElementById("bank-code");
const bankStatus = document.getElementById("bank-status");
const bankSuggestions = document.getElementById("bank-suggestions");
const bankOptions = document.getElementById("bank-options");
const accountNumber = document.getElementById("account-number");
const accountName = document.getElementById("account-name");
const accountNameStatus = document.getElementById("account-name-status");
const connectionStatus = document.getElementById("connection-status");

if (connectionStatus) {
  window.addEventListener("offline", () => {
    connectionStatus.textContent = "Your device is offline. Payment status cannot be confirmed; do not retry while a transaction is pending.";
  });
  window.addEventListener("online", () => {
    connectionStatus.textContent = "Your device is back online. This does not confirm a payment; check wallet history before retrying.";
  });
}

// Payout details form.
//
// The bank is identified from the account number rather than the other way
// round: Flutterwave can only tell us whose name is on an account once it
// knows which bank to ask, and it has no reverse lookup. So typing a full
// account number triggers a server-side sweep which returns the banks that
// number belongs to, and each of those carries the registered account name.
//
// The search field is the fallback. A number at a bank outside the server's
// shortlist resolves nowhere, and that user still has to be able to save their
// details by finding their bank by name.
if (bankSearch && bankCode && bankStatus && accountNumber && accountName) {
  // The full provider bank directory, used only for search and to turn a typed
  // name back into a code. Never sent back to the server except as bank_code.
  let bankDirectory = [];

  const setAccountNameStatus = (text, tone) => {
    if (!accountNameStatus) return;
    accountNameStatus.textContent = text;
    accountNameStatus.dataset.tone = tone ?? "";
  };

  const clearSuggestions = () => {
    if (!bankSuggestions) return;
    bankSuggestions.replaceChildren();
    bankSuggestions.hidden = true;
  };

  // The account name is the one an admin reads when approving a payout, so
  // once the provider has told us what it is, it stops being editable. Only a
  // bank the provider could not resolve leaves it open for typing.
  const setAccountNameVerified = (verified) => {
    accountName.readOnly = verified;
    accountName.classList.toggle("wallet-input-verified", verified);
  };

  const selectBank = (name, code, resolvedName) => {
    bankSearch.value = name;
    bankCode.value = code ?? "";
    // Marks the code as derived from the lookup rather than typed, so changing
    // the account number afterwards can clear it instead of leaving a code
    // that belongs to the previous number's bank.
    bankCode.dataset.fromLookup = "true";
    if (resolvedName) {
      accountName.value = resolvedName;
      setAccountNameVerified(true);
      setAccountNameStatus("Name confirmed with your bank.", "ok");
    } else {
      setAccountNameVerified(false);
      setAccountNameStatus("We could not confirm the name on this account. Type it exactly as your bank has it.", "warn");
    }
    clearSuggestions();
  };

  const renderSuggestions = (matches) => {
    if (!bankSuggestions) return;
    bankSuggestions.replaceChildren();
    if (!matches.length) {
      bankSuggestions.hidden = true;
      return;
    }

    const label = document.createElement("p");
    label.className = "wallet-bank-suggestions-label";
    label.textContent = matches.length === 1
      ? "This account number belongs to:"
      : "This account number is registered at more than one bank. Choose yours:";
    bankSuggestions.append(label);

    matches.forEach((match) => {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "wallet-bank-suggestion";

      const name = document.createElement("span");
      name.className = "wallet-bank-suggestion-name";
      name.textContent = match.name;

      const holder = document.createElement("span");
      holder.className = "wallet-bank-suggestion-holder";
      holder.textContent = match.account_name;

      button.append(name, holder);
      button.addEventListener("click", () => selectBank(match.name, match.code, match.account_name));
      bankSuggestions.append(button);
    });

    bankSuggestions.hidden = false;
  };

  // A sweep is several provider calls, so it is only worth starting on a
  // complete number, and only after typing pauses. The sequence guard stops a
  // slow response for an old number from overwriting a newer one.
  let lookupTimer = null;
  let lookupSequence = 0;

  const lookupAccountBanks = () => {
    const number = accountNumber.value.trim();
    const sequence = ++lookupSequence;

    if (!/^[0-9]{10}$/.test(number)) {
      clearSuggestions();
      // An incomplete number used to clear the status and stop, so typing nine
      // digits looked like nothing was happening until the browser blocked the
      // submit. Say the rule as soon as the number is not one.
      bankStatus.textContent = number === "" ? "" : "Nigerian bank account numbers must contain 10 digits.";
      if (number === "") {
        accountNumber.removeAttribute("aria-invalid");
      } else {
        accountNumber.setAttribute("aria-invalid", "true");
      }
      return;
    }

    accountNumber.removeAttribute("aria-invalid");

    bankStatus.textContent = "Finding the bank for this account number…";
    clearSuggestions();

    fetch("/wallet/lookup-account", {
      method: "POST",
      headers: {
        "Content-Type": "application/x-www-form-urlencoded",
        Accept: "application/json",
      },
      body: new URLSearchParams({ account_number: number }).toString(),
    })
      .then(async (response) => {
        const payload = await response.json().catch(() => ({}));
        if (!response.ok) throw new Error(payload.error || "We could not look up that account number.");
        return payload;
      })
      .then((payload) => {
        if (sequence !== lookupSequence) return;
        const matches = payload.banks ?? [];
        if (matches.length) {
          bankStatus.textContent = "";
          renderSuggestions(matches);
          return;
        }
        bankStatus.textContent = "We could not identify the bank for this number. Search for your bank below.";
        bankSearch.focus();
      })
      .catch((error) => {
        if (sequence !== lookupSequence) return;
        bankStatus.textContent = error.message;
      });
  };

  accountNumber.addEventListener("input", () => {
    window.clearTimeout(lookupTimer);
    // Changing the number invalidates everything the previous lookup produced:
    // the bank it identified, and the name registered there. Leaving the name
    // behind would submit a verified-looking name against a different account.
    if (bankCode.dataset.fromLookup === "true") {
      bankCode.value = "";
      bankCode.dataset.fromLookup = "false";
      accountName.value = "";
      setAccountNameVerified(false);
      setAccountNameStatus("", "");
    }
    lookupTimer = window.setTimeout(lookupAccountBanks, 350);
  });

  // Typing a bank name must keep bank_code in step: the server rejects a code
  // that is not the Flutterwave code for the selected bank, and there is no
  // way to recover the code from the name alone on the server side.
  bankSearch.addEventListener("input", () => {
    const typed = bankSearch.value.trim().toLowerCase();
    const match = bankDirectory.find((bank) => bank.name.toLowerCase() === typed);
    bankCode.value = match ? match.code : "";
    bankCode.dataset.fromLookup = "false";

    if (!match) {
      setAccountNameVerified(false);
      setAccountNameStatus("", "");
      return;
    }
    // Selecting a bank by hand leaves the name unverified, so the field stays
    // open, but say so plainly rather than letting the user assume it checked.
    setAccountNameStatus("We could not confirm the name on this account. Type it exactly as your bank has it.", "warn");
  });

  fetch("/wallet/banks", { headers: { Accept: "application/json" } })
    .then((response) => {
      if (!response.ok) throw new Error("The payment provider bank list is unavailable.");
      return response.json();
    })
    .then((banks) => {
      bankDirectory = banks;
      if (bankOptions) {
        bankOptions.replaceChildren(...banks.map((bank) => new Option(bank.name, bank.name)));
      }
      if (banks.length) return;

      if (bankStatus.textContent === "") {
        bankStatus.textContent = "No supported banks were returned. Try again in a moment.";
      }
    })
    .catch((error) => {
      // The search field is the fallback when a lookup resolves nothing, so a
      // failure here only matters if it is the only thing that worked.
      if (bankStatus.textContent === "") {
        bankStatus.textContent = error.message;
      }
    });
}

// "Send code" buttons for wallet actions that need a one-time passcode.
// The code always goes to the phone number on the account, so the browser only
// says which action it is for.
document.querySelectorAll("[data-otp-purpose]").forEach((button) => {
  button.addEventListener("click", () => {
    const status = document.getElementById(button.dataset.otpStatus);
    const target = document.getElementById(button.dataset.otpTarget);
    const setStatus = (text) => {
      if (status) status.textContent = text;
    };

    button.disabled = true;
    setStatus("Sending…");

    fetch("/wallet/otp/send", {
      method: "POST",
      headers: {
        "Content-Type": "application/x-www-form-urlencoded",
        Accept: "application/json",
      },
      body: new URLSearchParams({ purpose: button.dataset.otpPurpose }),
    })
      .then(async (response) => {
        const data = await response.json().catch(() => ({}));
        if (!response.ok) {
          throw new Error(data.error || "Could not send a verification code.");
        }
        setStatus(data.message || "Verification code sent.");
        if (target) target.focus();
      })
      .catch((error) => setStatus(error.message))
      .finally(() => {
        button.disabled = false;
      });
  });
});

// Permanent funding account: copy the account number without a round trip.
// The clipboard API is unavailable on some older mobile browsers, so fall back
// to selecting a temporary field. Neither path changes any server state.
document.querySelectorAll("[data-copy-target]").forEach((button) => {
  button.addEventListener("click", async () => {
    const source = document.getElementById(button.dataset.copyTarget);
    const status = document.getElementById("funding-copy-status");
    if (!source) return;

    const value = source.textContent.trim();
    const setStatus = (text) => {
      if (!status) return;
      status.textContent = text;
      window.setTimeout(() => {
        if (status.textContent === text) status.textContent = "";
      }, 4000);
    };

    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(value);
      } else {
        const field = document.createElement("textarea");
        field.value = value;
        field.setAttribute("readonly", "");
        field.style.position = "fixed";
        field.style.opacity = "0";
        document.body.appendChild(field);
        field.select();
        const copied = document.execCommand("copy");
        document.body.removeChild(field);
        if (!copied) throw new Error("copy failed");
      }
      setStatus("Account number copied.");
    } catch (error) {
      setStatus("Could not copy automatically. Select the number and copy it.");
    }
  });
});

// Identity forms submit as a normal POST so the server can redirect back with
// an honest result. Disabling the button only prevents a double tap; the form
// data is already being sent.
document.querySelectorAll("[data-virtual-account-form]").forEach((form) => {
  form.addEventListener("submit", () => {
    const submit = form.querySelector("[data-va-submit]");
    const status = document.getElementById("virtual-account-status");
    if (submit) {
      submit.disabled = true;
      submit.dataset.label = submit.textContent;
      submit.textContent = "Contacting Flutterwave…";
    }
    if (status) status.textContent = "Sending your verification details…";
  });
});

// Status checks are read-only but can be slow; keep the button from firing
// twice while the provider lookup is in flight.
document.querySelectorAll(".wallet-check-form").forEach((form) => {
  form.addEventListener("submit", () => {
    const submit = form.querySelector("button[type=submit]");
    if (submit) {
      submit.disabled = true;
      submit.textContent = "Checking…";
    }
  });
});

// ---------- Receipt modal ----------
//
// Every transaction row carries its details as a JSON blob on data-receipt.
// The modal is populated from that blob only — it never recomputes anything,
// never calls the server, and never moves money. The print path opens the
// same receipt in a clean window so the browser's print dialog can save it
// as a PDF.
(() => {
  const overlay = document.getElementById("receipt-overlay");
  if (!overlay) return;
  const receipt = overlay.querySelector(".wallet-receipt");
  const titleEl = document.getElementById("receipt-title");
  const metaEl = document.getElementById("receipt-meta");
  const amountEl = document.getElementById("receipt-amount");
  const stampEl = document.getElementById("receipt-stamp");
  const noteEl = document.getElementById("receipt-note");
  const closeBtn = document.getElementById("receipt-close");
  const doneBtn = document.getElementById("receipt-done");
  const printBtn = document.getElementById("receipt-print");

  let lastFocus = null;

  const PRETTY = {
    deposit: "Deposit", deposit_reversal: "Deposit reversal",
    sale_credit: "Sale credit", order_payment: "Order payment",
    withdrawal_reserved: "Withdrawal reserved", withdrawal_refund: "Withdrawal refund",
    transfer_in: "Transfer received", transfer_out: "Transfer sent",
    transfer_refund: "Transfer refund",
    Deposit: "Deposit", Withdrawal: "Withdrawal", Transfer: "Transfer", Activity: "Wallet activity",
  };

  const STATUS_STAMP = {
    successful: "PAID", paid: "PAID", completed: "COMPLETED",
    pending: "PENDING", processing: "PROCESSING",
    failed: "FAILED", rejected: "REJECTED",
    cancelled: "CANCELLED", expired: "EXPIRED",
    reversed: "REVERSED", Recorded: "RECORDED",
  };

  const pretty = (v) => (v && PRETTY[v]) || v || "—";

  const buildMeta = (data) => {
    const rows = [];
    rows.push(["Date", data.date]);
    rows.push(["Type", pretty(data.kind === "Activity" ? data.activity : data.kind)]);
    rows.push(["Direction", data.direction === "in" ? "Money in" : "Money out"]);
    rows.push(["Status", pretty(data.status)]);
    rows.push(["Reference", data.reference]);
    if (data.provider) rows.push(["Provider", data.provider]);
    if (data.verified) rows.push(["Verified amount", `₦${data.verified}`]);
    if (data.from) rows.push(["From", data.from]);
    if (data.to) rows.push(["To", data.to]);
    if (data.bank) rows.push(["Bank", data.bank]);
    if (data.accountName) rows.push(["Account name", data.accountName]);
    if (data.accountNumber) rows.push(["Account number", data.accountNumber]);
    if (data.note) rows.push(["Note", data.note]);
    return rows.filter(([, v]) => v && v !== "" && v !== "—");
  };

  const render = (data) => {
    titleEl.textContent = data.title || "Receipt";
    metaEl.replaceChildren();
    for (const [k, v] of buildMeta(data)) {
      const div = document.createElement("div");
      const dt = document.createElement("dt");
      dt.textContent = k;
      const dd = document.createElement("dd");
      dd.textContent = v;
      div.append(dt, dd);
      metaEl.append(div);
    }
    amountEl.textContent = data.amount || "₦0";
    amountEl.classList.toggle("wallet-receipt-amount-in", data.direction === "in");
    amountEl.classList.toggle("wallet-receipt-amount-out", data.direction === "out");
    const stamp = STATUS_STAMP[data.status] || "RECORDED";
    stampEl.textContent = stamp;
    stampEl.className = `wallet-receipt-stamp wallet-receipt-stamp-${stamp.toLowerCase()}`;
    noteEl.textContent = data.note ? `“${data.note}”` : "";
    noteEl.hidden = !data.note;
  };

  const open = (data, source) => {
    lastFocus = source;
    render(data);
    overlay.hidden = false;
    overlay.setAttribute("aria-hidden", "false");
    document.body.classList.add("wallet-receipt-open");
    closeBtn?.focus();
  };

  const close = () => {
    overlay.hidden = true;
    overlay.setAttribute("aria-hidden", "true");
    document.body.classList.remove("wallet-receipt-open");
    lastFocus?.focus?.();
    lastFocus = null;
  };

  document.querySelectorAll(".wallet-receipt-btn").forEach((btn) => {
    btn.addEventListener("click", () => {
      let data = {};
      try {
        data = JSON.parse(btn.dataset.receipt || "{}");
      } catch (e) {
        return;
      }
      open(data, btn);
    });
  });

  closeBtn?.addEventListener("click", close);
  doneBtn?.addEventListener("click", close);
  overlay.addEventListener("click", (e) => {
    if (e.target === overlay) close();
  });
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && !overlay.hidden) close();
  });

  printBtn?.addEventListener("click", () => {
    const html = `<!doctype html><html><head><meta charset="utf-8">
      <title>${titleEl.textContent} — Agro-Shield</title>
      <style>
        body { font-family: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; color: #183d29; background: #f8faf8; padding: 2rem; }
        .receipt { max-width: 540px; margin: 0 auto; background: #fff; border: 1px solid #e7e9e6; border-radius: 18px; padding: 2rem; position: relative; box-shadow: 0 10px 30px rgb(29 58 38 / 6%); }
        .brand { margin: 0; color: #2d7542; font-size: 0.78rem; font-weight: 800; letter-spacing: 0.18em; }
        h1 { margin: 0.3rem 0 1.2rem; color: #173d29; font-size: 1.6rem; letter-spacing: -0.03em; }
        dl { display: grid; margin: 0 0 1.4rem; }
        dl > div { display: flex; justify-content: space-between; gap: 1rem; padding: 0.5rem 0; border-bottom: 1px dashed #dcebdc; }
        dt { color: #6a766d; font-size: 0.78rem; font-weight: 700; text-transform: uppercase; letter-spacing: 0.02em; }
        dd { margin: 0; color: #173d28; font-size: 0.92rem; font-weight: 700; text-align: right; word-break: break-word; }
        .amount { display: flex; justify-content: space-between; align-items: baseline; padding: 1rem 1.2rem; border-radius: 14px; background: #f0f9ef; }
        .amount span { color: #6a766d; font-size: 0.85rem; font-weight: 700; }
        .amount strong { color: #173d28; font-size: 1.5rem; }
        .stamp { position: absolute; top: 1.4rem; right: 1.4rem; padding: 0.3rem 0.7rem; border: 2px solid #2d7542; border-radius: 8px; color: #2d7542; font-size: 0.78rem; font-weight: 900; letter-spacing: 0.14em; transform: rotate(8deg); }
        .foot { margin-top: 1.4rem; color: #7a847d; font-size: 0.78rem; line-height: 1.5; }
        .note { margin: 0.6rem 0 0; color: #5e7164; font-style: italic; font-size: 0.88rem; }
      </style></head><body>
      <div class="receipt">
        <p class="brand">AGRO-SHIELD</p>
        <h1>${titleEl.textContent}</h1>
        <div class="stamp">${stampEl.textContent}</div>
        <dl>${metaEl.innerHTML}</dl>
        <div class="amount"><span>Amount</span><strong>${amountEl.textContent}</strong></div>
        ${noteEl.textContent ? `<p class="note">${noteEl.textContent}</p>` : ""}
        <p class="foot">This receipt was generated from your Agro-Shield wallet history. The ledger entry above is already committed; this screen does not move money.</p>
      </div>
      <script>window.onload = () => setTimeout(() => window.print(), 250);</script>
      </body></html>`;
    const win = window.open("", "_blank", "width=640,height=720");
    if (!win) {
      noteEl.textContent = "Pop-ups are blocked. Allow pop-ups for this site to print or save the receipt.";
      noteEl.hidden = false;
      return;
    }
    win.document.open();
    win.document.write(html);
    win.document.close();
  });
})();

// ---------- Inflow / outflow summary ----------
//
// The wallet_transactions table already records direction in its type. The
// summary reads the rendered activity rows (which carry the direction in the
// badge class) and totals the amounts, so the farmer sees money in vs money
// out at a glance without any backend change.
(() => {
  const inflowEl = document.getElementById("summary-inflow");
  const outflowEl = document.getElementById("summary-outflow");
  const netEl = document.getElementById("summary-net");
  const table = document.querySelector(".wallet-full-history .table-cards");
  if (!table || !inflowEl || !outflowEl || !netEl) return;

  const parseAmount = (text) => {
    const m = text.replace(/[^0-9.]/g, "");
    const n = parseFloat(m);
    return Number.isFinite(n) ? n : 0;
  };

  let inflow = 0;
  let outflow = 0;
  table.querySelectorAll("tbody tr").forEach((row) => {
    const badge = row.querySelector(".wallet-direction");
    const amountCell = row.querySelector(".wallet-table-amount");
    if (!badge || !amountCell) return;
    const amt = parseAmount(amountCell.textContent);
    if (badge.classList.contains("wallet-direction-in")) inflow += amt;
    else outflow += amt;
  });

  const fmt = (n) => `₦${n.toLocaleString("en-NG", { maximumFractionDigits: 2 })}`;
  inflowEl.textContent = fmt(inflow);
  outflowEl.textContent = fmt(outflow);
  const net = inflow - outflow;
  netEl.textContent = fmt(Math.abs(net));
  netEl.classList.toggle("wallet-receipt-amount-in", net >= 0);
  netEl.classList.toggle("wallet-receipt-amount-out", net < 0);
})();

// ---------- Committed wallet refresh ----------
//
// A transfer can be confirmed after this page was rendered: the webhook
// verifies it with Flutterwave, commits the ledger entry and balance, and the
// page the farmer is looking at still shows the old number. Poll a read-only
// status endpoint and reload only when the server's committed state has
// actually moved. Nothing here credits, computes, or trusts a provider body -
// the endpoint reports what the database already holds.
//
// The poll runs only while the tab is visible, stops on pagehide, and each
// request is aborted after ten seconds so a stalled network cannot pile up
// requests. Returning to the tab checks immediately.
(() => {
  const page = document.querySelector(".wallet-page[data-latest-transaction-id]");
  if (!page) return;

  const POLL_INTERVAL_MS = 30000;
  const REQUEST_TIMEOUT_MS = 10000;

  const known = {
    balance: page.dataset.walletBalance ?? "",
    transactionID: page.dataset.latestTransactionId ?? "0",
    depositID: page.dataset.latestDepositId ?? "0",
    depositStatus: page.dataset.latestDepositStatus ?? "",
  };

  const note = document.getElementById("connection-status");
  const setNote = (text) => {
    if (!note) return;
    note.textContent = text;
    note.hidden = text === "";
  };

  let timer = null;
  let inFlight = false;
  let reloading = false;

  const stopPolling = () => {
    if (timer !== null) {
      window.clearInterval(timer);
      timer = null;
    }
  };

  const startPolling = () => {
    stopPolling();
    timer = window.setInterval(() => checkStatus(false), POLL_INTERVAL_MS);
  };

  const checkStatus = async (announce) => {
    if (inFlight || reloading || document.hidden) return;
    inFlight = true;

    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);
    try {
      const response = await fetch("/wallet/status", {
        headers: { Accept: "application/json" },
        credentials: "same-origin",
        cache: "no-store",
        signal: controller.signal,
      });
      if (!response.ok) return;

      const status = await response.json();
      const changed =
        String(status.balance ?? "") !== known.balance ||
        String(status.latest_transaction_id ?? "0") !== known.transactionID ||
        String(status.latest_deposit_id ?? "0") !== known.depositID ||
        String(status.latest_deposit_status ?? "") !== known.depositStatus;

      if (changed) {
        reloading = true;
        stopPolling();
        setNote("Your wallet changed. Refreshing your balance and history…");
        window.location.reload();
        return;
      }

      if (announce && Number(status.pending_deposits) > 0) {
        setNote("A deposit is still waiting for the payment provider. Your balance updates only after it is confirmed; do not send it again.");
      } else if (announce && !note?.textContent) {
        setNote("Wallet is up to date.");
      }
    } catch (error) {
      // A failed check changes nothing on screen and never implies success.
      // The next visible interval or focus event tries again.
    } finally {
      window.clearTimeout(timeout);
      inFlight = false;
    }
  };

  const schedule = () => {
    if (reloading) return;
    if (document.hidden) {
      stopPolling();
      return;
    }
    checkStatus(true);
    startPolling();
  };

  window.addEventListener("pageshow", (event) => {
    // bfcache can restore a page whose balance predates a confirmed credit.
    if (event.persisted) checkStatus(true);
    schedule();
  });
  document.addEventListener("visibilitychange", schedule);
  window.addEventListener("focus", () => checkStatus(true));
  window.addEventListener("online", () => checkStatus(true));
  window.addEventListener("pagehide", stopPolling);

  schedule();
})();
