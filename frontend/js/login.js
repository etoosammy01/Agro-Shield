const loginForm = document.getElementById("login-form");

const phone = document.getElementById("phone");
const password = document.getElementById("password");

const button = loginForm.querySelector("button[type='submit']");

// Create message container for client-side validation errors
const message = document.createElement("p");
message.className = "message";
loginForm.appendChild(message);

// ---------- Show Password ----------

const toggle = document.getElementById("toggle-password");

toggle.addEventListener("click", () => {
  if (password.type === "password") {
    password.type = "text";
    toggle.classList.remove("fa-eye");
    toggle.classList.add("fa-eye-slash");
  } else {
    password.type = "password";
    toggle.classList.remove("fa-eye-slash");
    toggle.classList.add("fa-eye");
  }
});

// ---------- Login ----------

loginForm.addEventListener("submit", function (event) {

  message.textContent = "";

  if (phone.value.trim() === "") {
    event.preventDefault();
    message.textContent = "Phone number is required.";
    message.style.color = "red";
    phone.focus();
    return;
  }

  if (password.value.length < 8) {
    event.preventDefault();
    message.textContent = "Password must be at least 8 characters.";
    message.style.color = "red";
    password.focus();
    return;
  }

  // Validation passed — let the form submit for real to POST /login.
  // The server checks the password and creates the session.
  button.disabled = true;
  button.textContent = "Signing In...";
});

// ---------- Background Images ----------

const loginImages = [
  "/static/assets/images/login-image1.jpeg",
  "/static/assets/images/login-image2.jpeg",
  "/static/assets/images/login-image3.jpeg",
  "/static/assets/images/login-image4.jpeg",
  "/static/assets/images/login-image5.jpeg"
];

const background = document.getElementById("auth-background");

let current = 0;

function changeBackground() {
  current++;
  if (current >= loginImages.length) current = 0;
  background.src = loginImages[current];
}

setInterval(changeBackground, 4000);

/* ============================================================
   NEW FEATURES ADDED - Better Error Handling
   ============================================================ */

// ---------- Forgot Password Link ----------
const forgotPassword = document.getElementById("forgot-password-link");

if (forgotPassword) {
    forgotPassword.addEventListener("click", function (event) {
        event.preventDefault();
        if (!phone.value.trim()) {
            message.textContent = "Please enter your phone number first.";
            message.style.color = "red";
            phone.focus();
            return;
        }
        // Simulate sending reset link (in production, this goes to backend)
        message.textContent = `Password reset link sent to ${phone.value.trim()}. Check your phone.`;
        message.style.color = "green";
    });
}

// ---------- Better Phone Validation ----------
phone.addEventListener("blur", () => {
    const phonePattern = /^\+?[0-9]{10,15}$/;
    if (phone.value.trim() && !phonePattern.test(phone.value.trim())) {
        message.textContent = "Please enter a valid phone number.";
        message.style.color = "red";
        phone.focus();
    }
});

// ---------- Password Length Check on Blur ----------
password.addEventListener("blur", () => {
    if (password.value.length > 0 && password.value.length < 8) {
        message.textContent = "Password must be at least 8 characters.";
        message.style.color = "red";
        password.focus();
    }
});