// ============================================================
// MARKETPLACE ENHANCEMENTS (Image Fix + Mobile Nav)
// ============================================================

document.addEventListener("DOMContentLoaded", () => {
    // ---------- 1. SEARCH FUNCTIONALITY ----------
    const search = document.getElementById("search");
    const grid = document.getElementById("product-grid");

    if (search && grid) {
        search.addEventListener("keyup", function () {
            const keyword = this.value.toLowerCase();
            const cards = grid.querySelectorAll(".product-card");

            cards.forEach(card => {
                const name = card.querySelector("h3")?.textContent.toLowerCase() || "";
                card.style.display = name.includes(keyword) ? "" : "none";
            });
        });
    }

    // ---------- 2. IMAGE FIX (Show uploaded product images) ----------
    const productImages = document.querySelectorAll(".product-image");
    
    productImages.forEach(img => {
        img.addEventListener("error", () => {
            img.src = "/static/assets/placeholder/product-placeholder.jpg";
            img.style.objectFit = "cover";
        });
    });

    // ---------- 3. MOBILE LEFT-SIDE NAV ----------
    const navToggle = document.getElementById("marketplace-nav-toggle");
    const nav = document.getElementById("marketplace-nav");
    const navClose = document.getElementById("marketplace-nav-close");

    if (navToggle && nav) {
        navToggle.addEventListener("click", () => {
            nav.classList.add("open");
            document.body.style.overflow = "hidden";
        });
    }

    if (navClose && nav) {
        navClose.addEventListener("click", () => {
            nav.classList.remove("open");
            document.body.style.overflow = "";
        });
    }

    // Close nav when clicking outside
    document.addEventListener("click", (event) => {
        if (nav && nav.classList.contains("open") && !nav.contains(event.target) && event.target !== navToggle) {
            nav.classList.remove("open");
            document.body.style.overflow = "";
        }
    });

    // ---------- 4. SELLER AVATAR PLACEHOLDER ----------
    const sellerIdentity = document.querySelectorAll(".seller-identity");
    
    sellerIdentity.forEach(identity => {
        const avatarPlaceholder = identity.querySelector("span");
        if (avatarPlaceholder && avatarPlaceholder.textContent === "👤") {
            avatarPlaceholder.textContent = "👨‍🌾";
            avatarPlaceholder.style.fontSize = "18px";
        }
    });
});
