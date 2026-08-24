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
