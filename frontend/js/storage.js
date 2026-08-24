const modal = document.getElementById("modal");
const addBtn = document.getElementById("add-btn");
const closeBtn = document.getElementById("close-btn");
const search = document.getElementById("search");
const form = document.getElementById("storage-form");
const modalTitle = document.getElementById("modal-title");
const formAction = document.getElementById("form-action");
const cropId = document.getElementById("crop-id");
const imageURL = document.getElementById("image-url");
const imageInput = document.getElementById("produce-image");
const imageHelp = document.getElementById("image-help");

function openModal(editProduct = null) {
    form.reset();
    if (editProduct) {
        modalTitle.textContent = "Edit Product";
        formAction.value = "update";
        cropId.value = editProduct.id;
        imageURL.value = editProduct.image;
        document.getElementById("produce").value = editProduct.name;
        document.getElementById("quantity").value = editProduct.quantity;
        document.getElementById("unit").value = editProduct.unit;
        document.getElementById("location").value = editProduct.location;
        document.getElementById("price").value = editProduct.price;
        document.getElementById("list-for-sale").checked = editProduct.listed === "true";
        imageInput.required = false;
        imageHelp.textContent = "Leave the photo empty to keep the current image.";
    } else {
        modalTitle.textContent = "Add Product";
        formAction.value = "create";
        cropId.value = "";
        imageURL.value = "";
        imageInput.required = true;
        imageHelp.textContent = "A product photo is required.";
    }
    modal.style.display = "flex";
}

if (addBtn) addBtn.addEventListener("click", () => openModal());

if (closeBtn) closeBtn.addEventListener("click", () => {
    modal.style.display = "none";
});

if (modal) modal.addEventListener("click", (event) => {
    if (event.target === modal) {
        modal.style.display = "none";
    }
});

if (search) search.addEventListener("keyup", function () {
    const keyword = this.value.toLowerCase();
    const rows = document.querySelectorAll("#storage-body tr");

    rows.forEach(row => {
        const produce = (row.dataset.productName || "").toLowerCase();
        row.style.display = produce.includes(keyword) ? "" : "none";
    });
});

document.addEventListener("click", event => {
    const button = event.target.closest(".edit-btn");
    if (button) openModal(button.dataset);
});

document.querySelectorAll(".delete-form").forEach(deleteForm => {
    deleteForm.addEventListener("submit", event => {
        if (!window.confirm("Delete this product permanently?")) event.preventDefault();
    });
});
