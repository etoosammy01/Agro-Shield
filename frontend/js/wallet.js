const bankSearch = document.getElementById("bank-search");
const bankCode = document.getElementById("bank-code");
const bankStatus = document.getElementById("bank-status");
const bankPicker = document.getElementById("bank-picker");
const bankMenu = document.getElementById("bank-menu");
const bankListbox = document.getElementById("bank-listbox");
const bankNoResults = document.getElementById("bank-no-results");
const accountNumber = document.getElementById("account-number");
const accountNumberStatus = document.getElementById("account-number-status");
const accountName = document.getElementById("account-name");
const accountNameStatus = document.getElementById("account-name-status");
const payoutSave = document.getElementById("payout-save");
const connectionStatus = document.getElementById("connection-status");

if (connectionStatus) {
  window.addEventListener("offline", () => {
    connectionStatus.textContent = "Your device is offline. Payment status cannot be confirmed; do not retry while a transaction is pending.";
  });
  window.addEventListener("online", () => {
    connectionStatus.textContent = "Your device is back online. This does not confirm a payment; check wallet history before retrying.";
  });
}

// Wallet-to-wallet transfer: the recipient is identified by phone number or
// Agro-Shield account number. A successful lookup fills the read-only account
// name; the server resolves the identifier again when the transfer is submitted,
// so this confirmation never becomes the authority for where money goes.
(() => {
  const recipient = document.getElementById("transfer-recipient");
  const submitButton = document.getElementById("transfer-submit");
  const status = document.getElementById("transfer-recipient-status");
  const name = document.getElementById("transfer-recipient-name");
  if (!recipient || !submitButton || !status || !name) return;

  const form = recipient.closest("form");
  let resolved = false;
  let sequence = 0;
  let lookupTimer = null;

  const setStatus = (message, tone = "") => {
    status.textContent = message;
    status.dataset.tone = tone;
  };

  const reset = (message = "", tone = "") => {
    sequence += 1;
    window.clearTimeout(lookupTimer);
    resolved = false;
    submitButton.disabled = true;
    name.value = "";
    name.classList.remove("wallet-input-verified");
    setStatus(message, tone);
  };

  const completeIdentifier = (digits) => (
    digits.length === 10 ||
    (digits.length === 11 && digits.startsWith("0")) ||
    (digits.length === 13 && digits.startsWith("234"))
  );

  async function resolveRecipient() {
    const identifier = recipient.value.trim();
    const digits = identifier.replace(/[^0-9]/g, "");
    const current = ++sequence;

    if (!completeIdentifier(digits)) {
      reset(identifier === "" ? "" : "Enter a complete phone number or 10-digit Agro-Shield account number.");
      return;
    }

    setStatus("Confirming the account name…");
    try {
      const response = await fetch("/wallet/transfer/recipient", {
        method: "POST",
        headers: {
          "Content-Type": "application/x-www-form-urlencoded",
          Accept: "application/json",
        },
        body: new URLSearchParams({ recipient: identifier }).toString(),
      });
      const payload = await response.json().catch(() => ({}));
      if (current !== sequence) return;
      if (!response.ok) {
        throw new Error(payload.error || "We could not find that Agro-Shield account.");
      }

      const recipientName = String(payload.name || "").trim();
      if (recipientName === "") {
        throw new Error("The account name was not returned. Try again.");
      }
      resolved = true;
      name.value = recipientName;
      name.classList.add("wallet-input-verified");
      submitButton.disabled = false;
      setStatus("Account name confirmed.", "ok");
    } catch (error) {
      if (current !== sequence) return;
      reset(error.message);
    }
  }

  recipient.addEventListener("input", () => {
    const digits = recipient.value.replace(/[^0-9]/g, "");
    reset();
    if (recipient.value.trim() === "") return;
    if (!completeIdentifier(digits)) {
      setStatus("Enter a complete phone number or 10-digit Agro-Shield account number.");
      return;
    }
    setStatus("Confirming the account name…");
    lookupTimer = window.setTimeout(resolveRecipient, 450);
  });

  if (form) {
    form.addEventListener("submit", (event) => {
      if (resolved) return;
      event.preventDefault();
      setStatus("Wait for the Agro-Shield account name to be confirmed before sending.", "warn");
      recipient.focus();
    });
  }
})();

// Payout details form.
//
// Flutterwave resolves an account name only when given both the account number
// and the bank code. When the user enters a number without choosing a bank, the
// server sweeps its bank shortlist and returns the matches: one match is filled
// in automatically, several are offered as suggestions, and none leaves the
// searchable full-directory picker as the fallback. Once a bank is chosen, one
// direct resolve fills in the verified name. No free-text account name is used.
if (bankSearch && bankCode && bankStatus && bankPicker && bankMenu && bankListbox && accountNumber && accountNumberStatus && accountName && accountNameStatus && payoutSave) {
  let bankDirectory = [];
  let selectedBank = null;
  let suggestedBanks = [];
  let bankAutoFilled = false;
  let visibleBanks = [];
  let activeIndex = -1;
  let banksLoading = true;
  let lookupTimer = null;
  let lookupSequence = 0;

  const normalize = (value) => value.toLowerCase().replace(/\s+/g, " ").trim();

  const setBankStatus = (text, tone = "") => {
    bankStatus.textContent = text;
    bankStatus.dataset.tone = tone;
  };

  const setAccountNameStatus = (text, tone = "") => {
    accountNameStatus.textContent = text;
    accountNameStatus.dataset.tone = tone;
  };

  const setAccountNumberStatus = (text, tone = "") => {
    accountNumberStatus.textContent = text;
    accountNumberStatus.dataset.tone = tone;
  };

  const setAccountNameVerified = (verified) => {
    accountName.readOnly = true;
    accountName.classList.toggle("wallet-input-verified", verified);
  };

  const syncSaveState = () => {
    payoutSave.disabled = !(
      bankCode.value !== "" &&
      bankSearch.value.trim() !== "" &&
      /^[0-9]{10}$/.test(accountNumber.value.trim()) &&
      accountName.value.trim() !== ""
    );
  };

  const resetResolvedName = (message = "", tone = "") => {
    lookupSequence += 1;
    window.clearTimeout(lookupTimer);
    accountName.value = "";
    setAccountNameVerified(false);
    setAccountNameStatus(message, tone);
    syncSaveState();
  };

  const optionID = (code) => `bank-option-${code.replace(/[^a-zA-Z0-9_-]/g, "")}`;

  const setActiveOption = (index) => {
    const options = bankListbox.querySelectorAll('[role="option"]');
    if (!options.length) {
      activeIndex = -1;
      bankSearch.removeAttribute("aria-activedescendant");
      return;
    }
    activeIndex = Math.max(0, Math.min(index, options.length - 1));
    options.forEach((option, optionIndex) => {
      option.classList.toggle("is-active", optionIndex === activeIndex);
    });
    const active = options[activeIndex];
    bankSearch.setAttribute("aria-activedescendant", active.id);
    active.scrollIntoView({ block: "nearest" });
  };

  const closeMenu = () => {
    bankMenu.hidden = true;
    bankPicker.classList.remove("is-open");
    bankSearch.setAttribute("aria-expanded", "false");
    bankSearch.removeAttribute("aria-activedescendant");
    activeIndex = -1;
  };

  const matchingBanks = (query) => {
    const needle = normalize(query);
    const suggestedCodes = new Set(suggestedBanks.map((bank) => bank.code));
    const isSuggested = (bank) => suggestedCodes.has(bank.code);

    if (needle === "") {
      return bankDirectory.slice().sort((a, b) => Number(isSuggested(b)) - Number(isSuggested(a)));
    }

    return bankDirectory
      .filter((bank) => normalize(bank.name).includes(needle) || bank.code.startsWith(needle))
      .sort((a, b) => {
        const aName = normalize(a.name);
        const bName = normalize(b.name);
        const aSuggestion = isSuggested(a) ? 0 : 1;
        const bSuggestion = isSuggested(b) ? 0 : 1;
        const aRank = aName.startsWith(needle) ? 0 : aName.split(" ").some((word) => word.startsWith(needle)) ? 1 : 2;
        const bRank = bName.startsWith(needle) ? 0 : bName.split(" ").some((word) => word.startsWith(needle)) ? 1 : 2;
        return aSuggestion - bSuggestion || aRank - bRank || aName.localeCompare(bName);
      });
  };

  const renderOptions = (query = "") => {
    visibleBanks = matchingBanks(query);
    const fragment = document.createDocumentFragment();

    visibleBanks.forEach((bank) => {
      const option = document.createElement("li");
      option.id = optionID(bank.code);
      option.className = "wallet-bank-option";
      option.setAttribute("role", "option");
      option.setAttribute("aria-selected", bank.code === bankCode.value ? "true" : "false");
      option.dataset.code = bank.code;

      const name = document.createElement("span");
      name.className = "wallet-bank-option-name";
      name.textContent = bank.name;

      const code = document.createElement("small");
      code.className = "wallet-bank-option-code";
      code.textContent = bank.code;

      option.append(name, code);
      option.addEventListener("mousedown", (event) => event.preventDefault());
      option.addEventListener("click", () => selectBank(bank));
      option.addEventListener("mousemove", () => {
        const index = visibleBanks.indexOf(bank);
        if (index !== activeIndex) setActiveOption(index);
      });
      fragment.append(option);
    });

    bankListbox.replaceChildren(fragment);
    bankNoResults.hidden = visibleBanks.length > 0;
    activeIndex = -1;
    bankSearch.removeAttribute("aria-activedescendant");

    const selectedIndex = visibleBanks.findIndex((bank) => bank.code === bankCode.value);
    if (selectedIndex >= 0) setActiveOption(selectedIndex);
  };

  const openMenu = () => {
    if (!bankDirectory.length) {
      if (!banksLoading && bankStatus.textContent === "") {
        setBankStatus("The bank list is not available yet. Check your connection and refresh the page.", "warn");
      }
      return;
    }
    const query = selectedBank && bankSearch.value.trim() === selectedBank.name ? "" : bankSearch.value;
    renderOptions(query);
    bankMenu.hidden = false;
    bankPicker.classList.add("is-open");
    bankSearch.setAttribute("aria-expanded", "true");
  };

  const scheduleLookup = () => {
    window.clearTimeout(lookupTimer);
    lookupTimer = window.setTimeout(resolveAccountName, 320);
  };

  function selectBank(bank) {
    if (!bank) return;
    const changed = bank.code !== bankCode.value || bank.name !== bankSearch.value;
    selectedBank = bank;
    bankAutoFilled = false;
    suggestedBanks = [];
    bankSearch.value = bank.name;
    bankCode.value = bank.code;
    bankSearch.removeAttribute("aria-invalid");
    setBankStatus("");
    setAccountNumberStatus("");
    closeMenu();

    if (changed) {
      resetResolvedName();
      if (/^[0-9]{10}$/.test(accountNumber.value.trim())) {
        setAccountNameStatus("Confirming the account name with your bank…");
        scheduleLookup();
      }
    }
    syncSaveState();
  }

  const applySuggestedBank = (match) => {
    selectedBank = bankDirectory.find((bank) => bank.code === match.code) || match;
    bankAutoFilled = true;
    suggestedBanks = [match];
    bankSearch.value = match.name;
    bankCode.value = match.code;
    bankSearch.removeAttribute("aria-invalid");
    setBankStatus(`Bank found from the account number: ${match.name}.`, "ok");
    setAccountNumberStatus("");
    closeMenu();

    if (match.accountName) {
      accountName.value = match.accountName;
      setAccountNameVerified(true);
      setAccountNameStatus(`Account name confirmed with ${match.name}.`, "ok");
    } else {
      accountName.value = "";
      setAccountNameVerified(false);
      setAccountNameStatus("Confirming the account name with your bank…");
      scheduleLookup();
    }
    syncSaveState();
  };

  async function suggestBankForAccount(number, sequence) {
    setAccountNameVerified(false);
    accountName.value = "";
    setAccountNumberStatus("Finding the bank for this account number…");
    setAccountNameStatus("");
    syncSaveState();

    try {
      const response = await fetch("/wallet/lookup-account", {
        method: "POST",
        headers: {
          "Content-Type": "application/x-www-form-urlencoded",
          Accept: "application/json",
        },
        body: new URLSearchParams({ account_number: number }).toString(),
      });
      const payload = await response.json().catch(() => ({}));
      if (sequence !== lookupSequence) return;
      if (!response.ok) throw new Error(payload.error || "We could not find the bank for that account number.");

      const matches = (Array.isArray(payload.banks) ? payload.banks : [])
        .map((match) => ({
          name: String((match && match.name) || "").trim(),
          code: String((match && match.code) || "").trim(),
          accountName: String((match && match.account_name) || "").trim(),
        }))
        .filter((match) => match.name !== "" && match.code !== "");

      if (matches.length === 0) {
        suggestedBanks = [];
        setAccountNumberStatus("No bank was found automatically. Search and choose your bank below.", "warn");
        return;
      }
      if (matches.length === 1) {
        applySuggestedBank(matches[0]);
        return;
      }

      suggestedBanks = matches;
      const names = matches.map((match) => match.name).join(", ");
      setAccountNumberStatus("This account number matches more than one bank. Choose the right one below.", "warn");
      setBankStatus(`Suggested banks: ${names}.`, "warn");
      if (document.activeElement === bankSearch) openMenu();
    } catch (error) {
      if (sequence !== lookupSequence) return;
      suggestedBanks = [];
      setAccountNameVerified(false);
      accountName.value = "";
      setAccountNumberStatus(error.message);
    } finally {
      if (sequence === lookupSequence) syncSaveState();
    }
  }

  async function resolveAccountName() {
    const number = accountNumber.value.trim();
    const code = bankCode.value.trim();
    const sequence = ++lookupSequence;

    if (!/^[0-9]{10}$/.test(number)) {
      setAccountNameVerified(false);
      setAccountNameStatus(number === "" ? "" : "Nigerian bank account numbers must contain 10 digits.");
      if (number === "") {
        accountNumber.removeAttribute("aria-invalid");
      } else {
        accountNumber.setAttribute("aria-invalid", "true");
      }
      syncSaveState();
      return;
    }

    accountNumber.removeAttribute("aria-invalid");
    if (!code) {
      await suggestBankForAccount(number, sequence);
      return;
    }

    setAccountNameVerified(false);
    accountName.value = "";
    setAccountNameStatus("Confirming the account name with your bank…");
    syncSaveState();

    let responseStatus = 0;
    try {
      const response = await fetch("/wallet/lookup-account", {
        method: "POST",
        headers: {
          "Content-Type": "application/x-www-form-urlencoded",
          Accept: "application/json",
        },
        body: new URLSearchParams({ account_number: number, bank_code: code }).toString(),
      });
      responseStatus = response.status;
      const payload = await response.json().catch(() => ({}));
      if (sequence !== lookupSequence) return;
      if (!response.ok) throw new Error(payload.error || "We could not confirm that account with your bank.");

      const resolvedName = String(payload.account_name || "").trim();
      if (resolvedName === "") throw new Error("Your bank did not return an account name. Try again.");
      accountName.value = resolvedName;
      setAccountNameVerified(true);
      setAccountNameStatus("Account name confirmed with your bank.", "ok");
    } catch (error) {
      if (sequence !== lookupSequence) return;
      // A selected bank that does not hold this number is usually a stale saved
      // bank or an accidental selection, not a reason to make the user clear the
      // field by hand. Ask the shortlist for the right bank instead.
      if (responseStatus === 422 && !bankAutoFilled) {
        await suggestBankForAccount(number, lookupSequence);
        return;
      }
      accountName.value = "";
      setAccountNameVerified(false);
      setAccountNameStatus(error.message);
    } finally {
      if (sequence === lookupSequence) syncSaveState();
    }
  }

  bankSearch.addEventListener("focus", openMenu);
  bankSearch.addEventListener("click", () => {
    if (bankMenu.hidden) openMenu();
  });
  bankSearch.addEventListener("input", () => {
    bankSearch.removeAttribute("aria-invalid");
    setBankStatus("");
    if (!selectedBank || bankSearch.value.trim() !== selectedBank.name) {
      selectedBank = null;
      bankCode.value = "";
      resetResolvedName();
    }
    openMenu();
    syncSaveState();
  });

  bankSearch.addEventListener("keydown", (event) => {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      if (bankMenu.hidden) openMenu();
      if (!visibleBanks.length) return;
      const next = event.key === "ArrowDown"
        ? (activeIndex < 0 ? 0 : activeIndex + 1)
        : (activeIndex < 0 ? visibleBanks.length - 1 : activeIndex - 1);
      setActiveOption((next + visibleBanks.length) % visibleBanks.length);
      return;
    }
    if (event.key === "Enter" && !bankMenu.hidden && activeIndex >= 0) {
      event.preventDefault();
      selectBank(visibleBanks[activeIndex]);
      return;
    }
    if (event.key === "Escape") {
      event.preventDefault();
      closeMenu();
      return;
    }
    if (event.key === "Home" && !bankMenu.hidden) {
      event.preventDefault();
      setActiveOption(0);
      return;
    }
    if (event.key === "End" && !bankMenu.hidden) {
      event.preventDefault();
      setActiveOption(visibleBanks.length - 1);
      return;
    }
    if (event.key === "Tab") closeMenu();
  });

  accountNumber.addEventListener("input", () => {
    const digits = accountNumber.value.replace(/[^0-9]/g, "").slice(0, 10);
    if (accountNumber.value !== digits) accountNumber.value = digits;
    accountNumber.removeAttribute("aria-invalid");

    if (bankAutoFilled) {
      bankAutoFilled = false;
      selectedBank = null;
      bankCode.value = "";
      bankSearch.value = "";
      setBankStatus("");
    }
    suggestedBanks = [];
    resetResolvedName();
    setAccountNumberStatus("");

    if (digits === "") {
      return;
    }
    if (digits.length < 10) {
      setAccountNumberStatus("Enter all 10 digits of your account number.");
      return;
    }

    if (bankCode.value) {
      setAccountNameStatus("Confirming the account name with your bank…");
    }
    scheduleLookup();
  });

  document.addEventListener("pointerdown", (event) => {
    if (!bankPicker.contains(event.target)) closeMenu();
  });

  const payoutForm = bankSearch.closest("form");
  if (payoutForm) {
    payoutForm.addEventListener("submit", (event) => {
      if (!payoutSave.disabled) return;
      event.preventDefault();
      if (!bankCode.value) {
        setBankStatus("Choose your bank or payment provider, or wait for the automatic bank suggestion.", "warn");
        bankSearch.focus();
        openMenu();
        return;
      }
      if (!/^[0-9]{10}$/.test(accountNumber.value.trim())) {
        setAccountNumberStatus("Enter all 10 digits of your account number.");
        accountNumber.focus();
        return;
      }
      setAccountNameStatus("Wait for your bank to confirm the account name before saving.", "warn");
    });
  }

  setAccountNameVerified(accountName.value.trim() !== "");
  syncSaveState();

  fetch("/wallet/banks", { headers: { Accept: "application/json" } })
    .then((response) => {
      if (!response.ok) throw new Error("The bank list could not be loaded. Check your connection and refresh the page.");
      return response.json();
    })
    .then((banks) => {
      banksLoading = false;
      const seenCodes = new Set();
      bankDirectory = (Array.isArray(banks) ? banks : []).filter((bank) => {
        if (!bank || !bank.code || !bank.name || seenCodes.has(bank.code)) return false;
        seenCodes.add(bank.code);
        return true;
      });

      selectedBank = bankDirectory.find((bank) => bank.code === bankCode.value) || null;
      if (!selectedBank && bankSearch.value.trim() !== "") {
        selectedBank = bankDirectory.find((bank) => normalize(bank.name) === normalize(bankSearch.value)) || null;
        if (selectedBank) bankCode.value = selectedBank.code;
      }
      setBankStatus(bankDirectory.length ? "" : "No banks or payment providers are available right now.", bankDirectory.length ? "" : "warn");
      syncSaveState();
      if (document.activeElement === bankSearch) openMenu();
    })
    .catch((error) => {
      banksLoading = false;
      setBankStatus(error.message, "warn");
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
    const status = document.getElementById(button.dataset.copyStatus || "funding-copy-status");
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

// Fund account: choose a method first. The amount is only asked on the debit
// card path; bank transfer shows the permanent account details immediately.
(() => {
  const dialog = document.getElementById("fund-account-dialog");
  if (!dialog) return;

  const views = Array.from(dialog.querySelectorAll("[data-funding-view]"));
  const show = (name) => {
    views.forEach((view) => {
      view.hidden = view.dataset.fundingView !== name;
    });
  };
  const open = () => {
    show("choose");
    if (typeof dialog.showModal === "function") {
      dialog.showModal();
    } else {
      dialog.setAttribute("open", "");
    }
  };
  const close = () => {
    if (typeof dialog.close === "function") {
      dialog.close();
    } else {
      dialog.removeAttribute("open");
    }
  };

  document.querySelectorAll("[data-open-funding]").forEach((button) => {
    button.addEventListener("click", open);
  });
  dialog.querySelectorAll("[data-funding-close]").forEach((button) => {
    button.addEventListener("click", close);
  });
  dialog.querySelectorAll("[data-funding-choice]").forEach((button) => {
    button.addEventListener("click", () => show(button.dataset.fundingChoice));
  });
  dialog.querySelectorAll("[data-funding-back]").forEach((button) => {
    button.addEventListener("click", () => show("choose"));
  });
  dialog.addEventListener("click", (event) => {
    if (event.target === dialog) close();
  });
  dialog.addEventListener("close", () => show("choose"));
})();

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
