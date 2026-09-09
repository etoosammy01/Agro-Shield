document.addEventListener("DOMContentLoaded", () => {
      const fileInput = document.getElementById("profile-picture");
      const continueButton = document.getElementById("continue-button");
      const preview = document.getElementById("photo-preview");
  
      if (!fileInput || !continueButton) return;
  
      fileInput.addEventListener("change", () => {
          const file = fileInput.files[0];
          if (file) {
              continueButton.disabled = false;
              const reader = new FileReader();
              reader.onload = (event) => {
                  preview.style.backgroundImage = `url(${event.target.result})`;
                  preview.classList.add("has-photo");
              };
              reader.readAsDataURL(file);
          } else {
              continueButton.disabled = true;
              preview.style.backgroundImage = "";
              preview.classList.remove("has-photo");
          }
      });
  });