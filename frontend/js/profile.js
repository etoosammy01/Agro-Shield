const editModal = document.getElementById("edit-modal");
const editBtn = document.getElementById("edit-profile-btn");
const cancelBtn = document.getElementById("cancel-edit-btn");

if (editBtn && editModal) {
    editBtn.addEventListener("click", () => {
        editModal.style.display = "flex";
    });
}

if (cancelBtn && editModal) {
    cancelBtn.addEventListener("click", () => {
        editModal.style.display = "none";
    });
}

if (editModal) {
    editModal.addEventListener("click", (event) => {
        if (event.target === editModal) {
            editModal.style.display = "none";
        }
    });

    if (window.location.hash === "#edit-modal") {
        editModal.style.display = "flex";
    }
}

// ---------- DELETE ACCOUNT ----------

const deleteModal = document.getElementById("delete-modal");
const deleteBtn = document.getElementById("delete-account-btn");
const cancelDeleteBtn = document.getElementById("cancel-delete-btn");
const deleteForm = document.getElementById("delete-account-form");
const confirmPhoneInput = document.getElementById("delete-confirm-phone");
const confirmPasswordInput = document.getElementById("delete-confirm-password");
const confirmDeleteBtn = document.getElementById("confirm-delete-btn");

function updateDeleteButtonState() {
    if (!confirmPhoneInput || !confirmPasswordInput || !confirmDeleteBtn) return;

    const expected = confirmPhoneInput.dataset.expected || "";
    const phoneMatches = confirmPhoneInput.value.trim() === expected.trim();
    const hasPassword = confirmPasswordInput.value.length > 0;

    confirmDeleteBtn.disabled = !(phoneMatches && hasPassword);
}

if (deleteBtn && deleteModal) {
    deleteBtn.addEventListener("click", () => {
        deleteModal.style.display = "flex";
    });
}

if (cancelDeleteBtn && deleteModal) {
    cancelDeleteBtn.addEventListener("click", () => {
        deleteModal.style.display = "none";
        if (deleteForm) deleteForm.reset();
        updateDeleteButtonState();
    });
}

if (deleteModal) {
    deleteModal.addEventListener("click", (event) => {
        if (event.target === deleteModal) {
            deleteModal.style.display = "none";
            if (deleteForm) deleteForm.reset();
            updateDeleteButtonState();
        }
    });
}

if (confirmPhoneInput) {
    confirmPhoneInput.addEventListener("input", updateDeleteButtonState);
}

if (confirmPasswordInput) {
    confirmPasswordInput.addEventListener("input", updateDeleteButtonState);
}

if (deleteForm) {
    deleteForm.addEventListener("submit", (event) => {
        if (confirmDeleteBtn && confirmDeleteBtn.disabled) {
            event.preventDefault();
            return;
        }

        // No native confirm() dialog — the typed phone number + password
        // above already serve as deliberate, attention-requiring
        // confirmation (the same "type X to confirm" pattern GitHub/GitLab
        // use for deleting repos). Instead, give clear in-app feedback
        // that the action registered and is processing.
        confirmDeleteBtn.disabled = true;
        confirmDeleteBtn.textContent = "Deleting...";
    });
}