
(function () {
    var guides = [
        { id: "poultry", name: "Poultry farming", icon: "🐔", sections: [
            ["Overview", "Raising chickens for eggs or meat. One of the fastest ways to start earning from livestock, with a short production cycle."],
            ["Getting started", "Choose broilers (meat) or layers (eggs). Start small — 20 to 50 birds — with a well-ventilated coop, clean water, and feeders."],
            ["Requirements", "Day-old chicks, starter feed, a brooder for warmth, vaccination schedule (Newcastle disease is critical), and clean bedding."],
            ["Common challenges", "Disease outbreaks, heat stress, and feed cost. Keep the coop clean and monitor birds daily for early signs of illness."]
        ]},
        { id: "rabbit", name: "Rabbit farming", icon: "🐇", sections: [
            ["Overview", "Rabbits breed fast and need little space, making them a good low-cost entry into livestock farming."],
            ["Getting started", "Start with one buck (male) and two to three does (females) in separate hutches to control breeding."],
            ["Requirements", "Raised hutches for ventilation and cleanliness, a diet of hay, pellets and fresh greens, and constant clean water."],
            ["Common challenges", "Overbreeding without a market plan, and digestive issues from sudden diet changes."]
        ]},
        { id: "fishery", name: "Fish farming", icon: "🐟", sections: [
            ["Overview", "Catfish and tilapia are the most common species farmed in ponds or tanks in Nigeria."],
            ["Getting started", "Choose between an earthen pond, concrete tank, or plastic tank based on your budget and land."],
            ["Requirements", "Clean, well-oxygenated water, quality fingerlings, floating feed, and regular water-quality checks."],
            ["Common challenges", "Poor water quality causing die-offs, and overfeeding which pollutes the pond."]
        ]},
        { id: "goat", name: "Goat farming", icon: "🐐", sections: [
            ["Overview", "Goats are hardy, low-maintenance, and valued for meat and milk."],
            ["Getting started", "Start with a small herd of local breeds, which are more disease-resistant than exotic breeds."],
            ["Requirements", "A dry, ventilated shelter, grazing land or fodder, and mineral/salt licks."],
            ["Common challenges", "Internal parasites and diseases spreading through overcrowded pens — deworm regularly."]
        ]},
        { id: "cattle", name: "Cattle rearing", icon: "🐄", sections: [
            ["Overview", "Cattle are a longer-term investment used for meat, milk, or draught power."],
            ["Getting started", "Decide your purpose (dairy or beef) before choosing a breed, and secure grazing land or fodder supply."],
            ["Requirements", "Ample grazing or feed, clean water, vaccination against common diseases, and shelter from extreme weather."],
            ["Common challenges", "High feed costs, disease outbreaks, and land availability."]
        ]},
        { id: "piggery", name: "Piggery", icon: "🐖", sections: [
            ["Overview", "Pigs grow fast and convert feed efficiently, making pig farming a profitable meat-production option."],
            ["Getting started", "Start with a few weaners in a simple pen with good drainage and a feeding/watering area."],
            ["Requirements", "Balanced feed, clean pens to prevent disease, and enough space per pig to avoid stress."],
            ["Common challenges", "Odour and waste management, and disease spread in crowded pens."]
        ]},
        { id: "beekeeping", name: "Beekeeping", icon: "🐝", sections: [
            ["Overview", "Beekeeping produces honey, wax, and improves crop pollination nearby."],
            ["Getting started", "Set up hives away from human traffic but near flowering plants and a water source."],
            ["Requirements", "A beehive (e.g. Langstroth or top-bar), protective gear, and a smoker for safe handling."],
            ["Common challenges", "Colony absconding due to disturbance, and seasonal nectar shortages."]
        ]},
        { id: "crop", name: "Crop farming", icon: "🌾", sections: [
            ["Overview", "Growing staple or cash crops such as maize, cassava, rice, or vegetables."],
            ["Getting started", "Test your soil, choose crops suited to your climate and season, and prepare land ahead of the rains."],
            ["Requirements", "Quality seeds, fertilizer, consistent water access, and pest/weed control."],
            ["Common challenges", "Pest infestations, unpredictable weather, and post-harvest losses — Agro-Shield's storage tools can help with the last one."]
        ]}
    ];

    var modal = document.getElementById("learnModal");
    var openBtn = document.getElementById("openLearnHub");
    var closeBtn = document.getElementById("learnModalClose");
    var backdrop = document.getElementById("learnModalBackdrop");
    var hubView = document.getElementById("learnHubView");
    var lessonView = document.getElementById("learnLessonView");
    var grid = document.getElementById("learnGrid");
    var emptyState = document.getElementById("learnEmptyState");
    var searchInput = document.getElementById("learnSearchInput");
    var backBtn = document.getElementById("learnBackBtn");
    var lessonContent = document.getElementById("learnLessonContent");

    if (!modal || !openBtn) return;

    function renderGrid(filter) {
        var term = (filter || "").trim().toLowerCase();
        var matches = guides.filter(function (g) {
            return g.name.toLowerCase().indexOf(term) !== -1;
        });

        grid.innerHTML = matches.map(function (g) {
            return '<button type="button" class="learn-topic-card" data-id="' + g.id + '">' +
                '<span class="learn-topic-icon">' + g.icon + '</span>' +
                '<span><h3>' + g.name + '</h3><p>' + g.sections.length + ' lessons</p></span>' +
                '</button>';
        }).join("");

        var noMatches = matches.length === 0 && term !== "";
        emptyState.hidden = !noMatches;
        grid.hidden = noMatches;

        var cards = grid.querySelectorAll(".learn-topic-card");
        for (var i = 0; i < cards.length; i++) {
            cards[i].addEventListener("click", function () {
                openLesson(this.getAttribute("data-id"));
            });
        }
    }

    function openLesson(id) {
        var guide = null;
        for (var i = 0; i < guides.length; i++) {
            if (guides[i].id === id) { guide = guides[i]; break; }
        }
        if (!guide) return;

        var html = '<p class="eyebrow">' + guide.icon + ' ' + guide.name + '</p>';
        for (var j = 0; j < guide.sections.length; j++) {
            html += '<div class="learn-lesson-section"><h4>' + guide.sections[j][0] + '</h4><p>' + guide.sections[j][1] + '</p></div>';
        }
        lessonContent.innerHTML = html;
        hubView.hidden = true;
        lessonView.hidden = false;
    }

    function openModal() {
        modal.hidden = false;
        document.body.style.overflow = "hidden";
        searchInput.value = "";
        renderGrid("");
        hubView.hidden = false;
        lessonView.hidden = true;
    }

    function closeModal() {
        modal.hidden = true;
        document.body.style.overflow = "";
    }

    openBtn.addEventListener("click", openModal);
    closeBtn.addEventListener("click", closeModal);
    backdrop.addEventListener("click", closeModal);
    backBtn.addEventListener("click", function () {
        lessonView.hidden = true;
        hubView.hidden = false;
    });
    searchInput.addEventListener("input", function (e) { renderGrid(e.target.value); });
    document.addEventListener("keydown", function (e) {
        if (e.key === "Escape" && !modal.hidden) closeModal();
    });
})();
