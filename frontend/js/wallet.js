const bankSelect = document.getElementById("bank-name");
const bankCode = document.getElementById("bank-code");
const bankStatus = document.getElementById("bank-status");
const connectionStatus = document.getElementById("connection-status");

if (connectionStatus) {
  window.addEventListener("offline", () => {
    connectionStatus.textContent = "Your device is offline. Payment status cannot be confirmed; do not retry while a transaction is pending.";
  });
  window.addEventListener("online", () => {
    connectionStatus.textContent = "Your device is back online. This does not confirm a payment; check wallet history before retrying.";
  });
}

if (bankSelect && bankCode && bankStatus) {
  fetch("/wallet/banks", { headers: { Accept: "application/json" } })
    .then((response) => {
      if (!response.ok) throw new Error("The payment provider bank list is unavailable.");
      return response.json();
    })
    .then((banks) => {
      bankSelect.replaceChildren(new Option("Select your bank", ""));
      banks.forEach((bank) => {
        const option = new Option(bank.name, bank.name);
        option.dataset.code = bank.code;
        if (bank.name === bankSelect.dataset.selected && bank.code === bankSelect.dataset.selectedCode) {
          option.selected = true;
        }
        bankSelect.add(option);
      });
      bankStatus.textContent = banks.length ? "" : "No supported banks were returned.";
    })
    .catch((error) => {
      bankSelect.replaceChildren(new Option("Could not load supported banks", ""));
      bankStatus.textContent = error.message;
    });

  bankSelect.addEventListener("change", () => {
    bankCode.value = bankSelect.selectedOptions[0]?.dataset.code ?? "";
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
