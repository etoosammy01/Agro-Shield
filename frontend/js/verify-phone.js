document.addEventListener("DOMContentLoaded", () => {
    let submitting = false;
    const forms = document.querySelectorAll("form[data-verification-form]");
    forms.forEach((form) => {
        form.addEventListener("submit", (event) => {
            if (submitting) { event.preventDefault(); return; }
            submitting = true;
            // Actions are hidden fields, so disabling the button preserves them.
            forms.forEach((verificationForm) => {
                verificationForm.querySelectorAll('button[type="submit"]').forEach((button) => {
                    button.disabled = true;
                });
            });
            form.querySelector('button[type="submit"]').textContent = "Please wait…";
        });
    });
    window.addEventListener("pageshow", (event) => {
        if (event.persisted) window.location.reload();
    });
});
