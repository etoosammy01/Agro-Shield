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
/* ============================================================
   NEW FEATURES ADDED - Mobile Left-Side Nav Toggle
   ============================================================ */

(function() {
    const navToggle = document.querySelector(".marketplace-nav-toggle");
    const nav = document.querySelector(".marketplace-nav");
    const navClose = document.querySelector(".marketplace-nav-close");

    if (navToggle && nav) {
        navToggle.addEventListener("click", () => {
            nav.classList.add("open");
            document.body.style.overflow = "hidden"; // Prevent background scroll
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
})();
