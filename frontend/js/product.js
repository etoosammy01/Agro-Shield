// ============================================================
// AGRO-SHIELD PRODUCT PAGE
// ============================================================

document.addEventListener("DOMContentLoaded", () => {
    initProductGallery();
    initImageLightbox();
    initBackButton();
});

// ============================================================
// PRODUCT IMAGE GALLERY
// ============================================================

function initProductGallery() {
    const images = document.querySelectorAll(".product-gallery-image");
    const mainImage = document.querySelector(".product-main-image");
    const thumbnails = document.querySelectorAll(".product-thumbnail");

    if (!mainImage || images.length === 0) {
        return;
    }

    thumbnails.forEach((thumbnail, index) => {
        thumbnail.addEventListener("click", () => {
            const image = images[index];

            if (!image) {
                return;
            }

            mainImage.src = image.src;
            mainImage.alt = image.alt || "Product image";

            thumbnails.forEach(item => {
                item.classList.remove("active");
            });

            thumbnail.classList.add("active");
        });
    });
}

// ============================================================
// IMAGE LIGHTBOX
// ============================================================

function initImageLightbox() {
    const mainImage = document.querySelector(".product-main-image");

    if (!mainImage) {
        return;
    }

    mainImage.addEventListener("click", () => {
        if (!mainImage.src) {
            return;
        }

        createLightbox(mainImage.src, mainImage.alt);
    });
}

function createLightbox(imageSrc, imageAlt) {
    const existingLightbox = document.querySelector(".product-lightbox");

    if (existingLightbox) {
        existingLightbox.remove();
    }

    const lightbox = document.createElement("div");
    lightbox.className = "product-lightbox";

    lightbox.innerHTML = `
        <button
            class="lightbox-close"
            type="button"
            aria-label="Close image"
        >
            &times;
        </button>

        <img
            class="lightbox-image"
            src="${escapeHTML(imageSrc)}"
            alt="${escapeHTML(imageAlt || "Product image")}"
        >
    `;

    document.body.appendChild(lightbox);

    document.body.style.overflow = "hidden";

    const closeButton = lightbox.querySelector(".lightbox-close");

    closeButton.addEventListener("click", closeLightbox);

    lightbox.addEventListener("click", event => {
        if (event.target === lightbox) {
            closeLightbox();
        }
    });

    document.addEventListener("keydown", handleLightboxKeydown);
}

function closeLightbox() {
    const lightbox = document.querySelector(".product-lightbox");

    if (!lightbox) {
        return;
    }

    lightbox.remove();

    document.body.style.overflow = "";

    document.removeEventListener("keydown", handleLightboxKeydown);
}

function handleLightboxKeydown(event) {
    if (event.key === "Escape") {
        closeLightbox();
    }
}

// ============================================================
// BACK BUTTON
// ============================================================

function initBackButton() {
    const backButton = document.querySelector(".back-button");

    if (!backButton) {
        return;
    }

    backButton.addEventListener("click", event => {
        /*
         * If the user came from another page inside Agro-Shield,
         * browser history gives them a more natural way back.
         *
         * Otherwise, the normal href on the button is used.
         */
        if (
            document.referrer &&
            document.referrer.includes(window.location.origin)
        ) {
            event.preventDefault();
            window.history.back();
        }
    });
}

// ============================================================
// IMAGE ERROR HANDLING
// ============================================================

document.addEventListener("error", event => {
    const image = event.target;

    if (!(image instanceof HTMLImageElement)) {
        return;
    }

    if (!image.classList.contains("product-main-image")) {
        return;
    }

    image.classList.add("image-error");

    image.alt = "Product image unavailable";
}, true);

// ============================================================
// HTML ESCAPING
// ============================================================
//
// Prevents image URLs or alt text from being inserted as raw HTML.
// ============================================================

function escapeHTML(value) {
    return String(value)
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#039;");
}