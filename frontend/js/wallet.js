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
