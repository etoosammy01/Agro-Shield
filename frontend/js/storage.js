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
const captureLocation = document.getElementById("capture-location");
const locationStatus = document.getElementById("location-status");

// ---------- CLOUDINARY CONFIG (Replace with your own) ----------
const CLOUDINARY_CLOUD_NAME = "your-cloud-name";
const CLOUDINARY_UPLOAD_PRESET = "your-upload-preset";

// ---------- FILE VALIDATION ----------
const VALID_IMAGE_TYPES = ["image/jpeg", "image/png", "image/webp", "image/gif"];
const MAX_IMAGE_SIZE = 5 * 1024 * 1024; // 5MB

function validateImageFile(file) {
    if (!file) return { valid: false, error: "No file selected." };
    
    if (!VALID_IMAGE_TYPES.includes(file.type)) {
        return { valid: false, error: "Invalid image type. Please upload JPG, PNG, WEBP, or GIF." };
    }
    
    if (file.size > MAX_IMAGE_SIZE) {
        return { valid: false, error: "Image too large. Maximum size is 5MB." };
    }
    
    return { valid: true };
}

// ---------- CLOUDINARY UPLOAD ----------
async function uploadToCloudinary(file) {
    const formData = new FormData();
    formData.append("file", file);
    formData.append("upload_preset", CLOUDINARY_UPLOAD_PRESET);
    
    const response = await fetch(
        `https://api.cloudinary.com/v1_1/${CLOUDINARY_CLOUD_NAME}/image/upload`,
        { method: "POST", body: formData }
    );
    
    if (!response.ok) {
        throw new Error("Image upload failed.");
    }
    
    const data = await response.json();
    return data.secure_url;
}

// ---------- GPS CAPTURE ----------
function captureGPS() {
    if (!navigator.geolocation) { 
        locationStatus.textContent = "This device does not support GPS."; 
        return; 
    }
    locationStatus.textContent = "Getting your farm location...";
    navigator.geolocation.getCurrentPosition(position => {
        document.getElementById("latitude").value = position.coords.latitude;
        document.getElementById("longitude").value = position.coords.longitude;
        document.getElementById("location-accuracy").value = position.coords.accuracy || "";
        locationStatus.textContent = `Location captured (accuracy ${Math.round(position.coords.accuracy)} m)`;
    }, () => { 
        locationStatus.textContent = "Location permission is required to register a crop."; 
    }, { enableHighAccuracy: true, timeout: 15000, maximumAge: 30000 });
}

if (captureLocation) captureLocation.addEventListener("click", captureGPS);

if (form) form.addEventListener("submit", async event => { 
    if (!document.getElementById("latitude").value || !document.getElementById("longitude").value) { 
        event.preventDefault(); 
        captureGPS(); 
        return;
    }

    // Handle image upload
    const imageFile = imageInput.files[0];
    
    if (imageFile) {
        const validation = validateImageFile(imageFile);
        if (!validation.valid) {
            event.preventDefault();
            imageHelp.textContent = validation.error;
            return;
        }
        
        // Show uploading status
        imageHelp.textContent = "Uploading image to Cloudinary...";
        
        try {
            const uploadedUrl = await uploadToCloudinary(imageFile);
            imageURL.value = uploadedUrl;
            imageHelp.textContent = "✅ Image uploaded successfully.";
        } catch (error) {
            event.preventDefault();
            imageHelp.textContent = "Image upload failed. Please try again.";
            console.error(error);
        }
    }
});

// ---------- OPEN MODAL ----------
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
        document.getElementById("latitude").value = editProduct.latitude || "";
        document.getElementById("longitude").value = editProduct.longitude || "";
        document.getElementById("location-accuracy").value = editProduct.accuracy || "";
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

// ---------- SEARCH ----------
if (search) search.addEventListener("keyup", function () {
    const keyword = this.value.toLowerCase();
    const rows = document.querySelectorAll("#storage-body tr");
    rows.forEach(row => {
        const produce = (row.dataset.productName || "").toLowerCase();
        row.style.display = produce.includes(keyword) ? "" : "none";
    });
});

// ---------- EDIT BUTTONS ----------
document.addEventListener("click", event => {
    const button = event.target.closest(".edit-btn");
    if (button) openModal(button.dataset);
});

// ---------- DELETE CONFIRMATION ----------
document.querySelectorAll(".delete-form").forEach(deleteForm => {
    deleteForm.addEventListener("submit", event => {
        if (!window.confirm("Delete this product permanently?")) event.preventDefault();
    });
});

// ---------- IMAGE INPUT VALIDATION ----------
if (imageInput) {
    imageInput.addEventListener("change", () => {
        const file = imageInput.files[0];
        const validation = validateImageFile(file);
        if (!validation.valid) {
            imageHelp.textContent = validation.error;
            imageInput.value = "";
        } else {
            imageHelp.textContent = "Image selected. Will upload on submit.";
        }
    });
}