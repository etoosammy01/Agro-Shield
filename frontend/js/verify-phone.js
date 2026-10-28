document.addEventListener("DOMContentLoaded", () => {
    let submitting = false;
    document.querySelectorAll('form[action="/verify-phone"]').forEach((form) => {
        form.addEventListener("submit", (event) => {
            if (submitting) { event.preventDefault(); return; }
            submitting = true;
            // Actions are hidden fields, so disabling the button preserves them.
            document.querySelectorAll('form[action="/verify-phone"] button[type="submit"]').forEach((button) => {
                button.disabled = true;
            });
            form.querySelector('button[type="submit"]').textContent = "Please wait…";
        });
    });
    window.addEventListener("pageshow", (event) => {
        if (event.persisted) window.location.reload();
    });
});
