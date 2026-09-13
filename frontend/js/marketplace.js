// ============================================================
// MARKETPLACE ENHANCEMENTS (Search + Image Fix + Mobile Nav)
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

    // ---------- 2. IMAGE FIX (fallback for broken AND stalled images) ----------
    const IMAGE_TIMEOUT_MS = 6000;
    const PLACEHOLDER_SRC = "/static/assets/placeholder/product-placeholder.jpg";

    const productImages = document.querySelectorAll(".product-image");

    productImages.forEach(img => {
        const fallback = () => {
            if (img.src.indexOf(PLACEHOLDER_SRC) === -1) {
                img.src = PLACEHOLDER_SRC;
            }
        };

        img.addEventListener("error", fallback);

        // If the request just hangs (slow/unreliable connection) rather than
        // failing outright, onerror never fires and the browser keeps the
        // page's loading indicator active. Force a fallback after a timeout
        // so a stalled image never blocks the page from finishing load.
        if (!img.complete) {
            const timer = setTimeout(() => {
                if (!img.complete) {
                    fallback();
                }
            }, IMAGE_TIMEOUT_MS);

            img.addEventListener("load", () => clearTimeout(timer));
            img.addEventListener("error", () => clearTimeout(timer));
        }
    });

    // ---------- 3. MOBILE LEFT-SIDE NAV ----------
    const navToggle = document.querySelector(".marketplace-nav-toggle");
    const nav = document.querySelector(".marketplace-nav");
    const navClose = document.querySelector(".marketplace-nav-close");

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

    document.addEventListener("click", (event) => {
        if (nav && nav.classList.contains("open") && !nav.contains(event.target) && event.target !== navToggle) {
            nav.classList.remove("open");
            document.body.style.overflow = "";
        }
    });

});