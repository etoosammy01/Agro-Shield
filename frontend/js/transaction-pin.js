document.addEventListener("DOMContentLoaded", () => {
    const form = document.getElementById("transaction-pin-form");
    const pin = document.getElementById("transaction-pin");
    const confirmation = document.getElementById("confirm-transaction-pin");
    if (!form || !pin || !confirmation) return;

    let submitting = false;
    const validateConfirmation = () => {
        confirmation.setCustomValidity(confirmation.value && pin.value !== confirmation.value
            ? "The transaction PINs do not match." : "");
    };
    pin.addEventListener("input", validateConfirmation);
    confirmation.addEventListener("input", validateConfirmation);
    form.addEventListener("submit", (event) => {
        validateConfirmation();
        if (submitting || !form.reportValidity()) {
            event.preventDefault();
            return;
        }
        submitting = true;
        const button = form.querySelector('button[type="submit"]');
        button.disabled = true;
        button.textContent = "Saving PIN…";
    });
    window.addEventListener("pageshow", (event) => {
        if (event.persisted) window.location.reload();
    });
});
