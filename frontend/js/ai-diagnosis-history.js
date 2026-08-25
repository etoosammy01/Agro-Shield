// =========================================
// AI DIAGNOSIS HISTORY
// =========================================


// -----------------------------------------
// ELEMENTS
// -----------------------------------------

const searchInput =
    document.getElementById("history-search");

const categoryFilter =
    document.getElementById("category-filter");

const diagnosisCards =
    document.querySelectorAll(".diagnosis-card");

const resultCount =
    document.getElementById("result-count");

const noSearchResults =
    document.getElementById("no-search-results");

document.querySelectorAll(".delete-diagnosis-form").forEach(form => {
    form.addEventListener("submit", event => {
        if (!window.confirm("Delete this AI diagnosis permanently?")) {
            event.preventDefault();
        }
    });
});


// -----------------------------------------
// FILTER HISTORY
// -----------------------------------------

function filterHistory() {

    const searchTerm =
        searchInput.value
            .trim()
            .toLowerCase();

    const selectedCategory =
        categoryFilter.value;

    let visibleCount = 0;


    diagnosisCards.forEach(card => {

        const category =
            card.dataset.category || "";

        const searchableText =
            card.dataset.search || "";


        const matchesSearch =
            searchableText
                .toLowerCase()
                .includes(searchTerm);


        const matchesCategory =
            selectedCategory === "all" ||
            category === selectedCategory;


        if (matchesSearch && matchesCategory) {

            card.style.display = "";

            visibleCount++;

        } else {

            card.style.display = "none";

        }

    });


    // Update count

    resultCount.textContent =
        visibleCount.toLocaleString();


    // Show empty search result

    if (noSearchResults) {

        if (visibleCount === 0) {

            noSearchResults.hidden = false;

        } else {

            noSearchResults.hidden = true;

        }

    }

}


// -----------------------------------------
// EVENT LISTENERS
// -----------------------------------------

if (searchInput) {

    searchInput.addEventListener(
        "input",
        filterHistory
    );

}


if (categoryFilter) {

    categoryFilter.addEventListener(
        "change",
        filterHistory
    );

}



// =========================================
// RESULT MODAL
// =========================================

const modal =
    document.getElementById("result-modal");

const modalOverlay =
    document.getElementById("modal-overlay");

const modalClose =
    document.getElementById("modal-close");

const modalTitle =
    document.getElementById("modal-title");

const modalDate =
    document.getElementById("modal-date");

const modalCategory =
    document.getElementById("modal-category");

const modalDescription =
    document.getElementById("modal-description");

const modalResult =
    document.getElementById("modal-result");


const viewButtons =
    document.querySelectorAll(
        ".view-result-button"
    );


// -----------------------------------------
// OPEN MODAL
// -----------------------------------------

function openResultModal(card) {

    const category =
        card.dataset.category || "";


    const categoryElement =
        card.querySelector(
            ".diagnosis-category h2"
        );


    const dateElement =
        card.querySelector(
            ".diagnosis-category time"
        );


    const descriptionElement =
        card.querySelector(
            ".diagnosis-description p"
        );


    const resultElement =
        card.querySelector(
            ".result-content"
        );


    modalTitle.textContent =
        categoryElement
            ? categoryElement.textContent.trim()
            : "AI Diagnosis";


    modalDate.textContent =
        dateElement
            ? dateElement.textContent.trim()
            : "";


    modalCategory.textContent =
        category;


    modalDescription.textContent =
        descriptionElement
            ? descriptionElement.textContent.trim()
            : "No description was provided.";


    modalResult.textContent =
        resultElement
            ? resultElement.textContent.trim()
            : "";


    modal.hidden = false;

    document.body.classList.add(
        "modal-open"
    );

}


// -----------------------------------------
// CLOSE MODAL
// -----------------------------------------

function closeResultModal() {

    modal.hidden = true;

    document.body.classList.remove(
        "modal-open"
    );

}


// -----------------------------------------
// VIEW RESULT BUTTONS
// -----------------------------------------

viewButtons.forEach(button => {

    button.addEventListener(
        "click",
        () => {

            const card =
                button.closest(
                    ".diagnosis-card"
                );

            if (card) {

                openResultModal(card);

            }

        }
    );

});


// -----------------------------------------
// CLOSE EVENTS
// -----------------------------------------

if (modalClose) {

    modalClose.addEventListener(
        "click",
        closeResultModal
    );

}


if (modalOverlay) {

    modalOverlay.addEventListener(
        "click",
        closeResultModal
    );

}


// -----------------------------------------
// ESCAPE KEY
// -----------------------------------------

document.addEventListener(
    "keydown",
    event => {

        if (
            event.key === "Escape" &&
            modal &&
            !modal.hidden
        ) {

            closeResultModal();

        }

    }
);


// -----------------------------------------
// PREVENT BACKGROUND SCROLL
// -----------------------------------------

const originalBodyOverflow =
    document.body.style.overflow;


const observer =
    new MutationObserver(() => {

        if (modal.hidden) {

            document.body.style.overflow =
                originalBodyOverflow;

        } else {

            document.body.style.overflow =
                "hidden";

        }

    });


if (modal) {

    observer.observe(
        modal,
        {
            attributes: true,
            attributeFilter: ["hidden"]
        }
    );

}


// =========================================
// INITIAL COUNT
// =========================================

filterHistory();
