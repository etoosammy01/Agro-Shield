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
