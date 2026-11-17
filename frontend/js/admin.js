"use strict";

(() => {
  const effects = {
    suspend: "This signs the member out and hides their listings. Reactivation requires a new sign-in.",
    reactivate: "This restores account access. The member must sign in again. Individually blocked listings stay blocked.",
    role_none: "This removes the staff role. Wallet approval access is managed separately.",
    role_moderator: "This allows member lookup and listing moderation. Moderators cannot change accounts, assign roles, or read the audit log.",
    role_administrator: "This allows account management, staff assignments, listing moderation, and audit review. Wallet approval access is managed separately.",
    wallet_grant: "This grants access to review and approve wallet withdrawals and transfers.",
    wallet_revoke: "This removes wallet approval access. The member’s staff role stays the same."
  };

  document.querySelectorAll("[data-dialog-target]").forEach(button => {
    button.addEventListener("click", () => {
      const dialog = document.getElementById(button.dataset.dialogTarget);
      if (dialog) dialog.showModal();
    });
  });
  document.querySelectorAll("[data-close-dialog]").forEach(button => {
    button.addEventListener("click", () => button.closest("dialog").close());
  });
  document.querySelectorAll(".change-form").forEach(form => {
    const action = form.querySelector('[name="action"]');
    const effect = form.querySelector("[data-action-effect]");
    const confirmation = form.querySelector("[data-review-confirmation]");
    const update = () => {
      if (effect) effect.textContent = effects[action.value] || "Select a change to see what it affects.";
      confirmation.checked = false;
    };
    form.querySelectorAll("select").forEach(select => select.addEventListener("change", update));
    form.addEventListener("submit", event => {
      if (!form.reportValidity()) { event.preventDefault(); return; }
      const button = form.querySelector('[type="submit"]');
      if (button.disabled) { event.preventDefault(); return; }
      button.dataset.originalLabel = button.textContent;
      button.disabled = true;
      button.textContent = "Saving…";
    });
    form.closest("dialog").addEventListener("close", () => { form.reset(); update(); });
  });
  // Browsers can restore a submitted page from their back/forward cache.
  window.addEventListener("pageshow", () => {
    document.querySelectorAll("button[data-original-label]").forEach(button => {
      button.disabled = false;
      button.textContent = button.dataset.originalLabel;
    });
  });
})();
