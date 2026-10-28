document.addEventListener("DOMContentLoaded", () => {
    const form = document.getElementById("complete-profile-form");
    const video = document.getElementById("selfie-video");
    const preview = document.getElementById("selfie-preview");
    const capture = document.getElementById("capture-button");
    const retake = document.getElementById("retake-button");
    const retry = document.getElementById("camera-retry-button");
    const next = document.getElementById("continue-button");
    const status = document.getElementById("camera-status");
    if (!form || !video || !preview || !capture || !retake || !retry || !next || !status) return;

    let stream = null;
    let photo = null;
    let photoURL = null;
    let opening = false;
    let submitting = false;
    let active = true;

    function message(text, error = false) {
        status.textContent = text;
        status.classList.toggle("is-error", error);
    }

    function stopCamera() {
        if (stream) stream.getTracks().forEach((track) => track.stop());
        stream = null;
        video.srcObject = null;
    }

    async function startCamera() {
        if (opening || submitting || !active) return;
        opening = true;
        stopCamera();
        photo = null;
        if (photoURL) URL.revokeObjectURL(photoURL);
        photoURL = null;
        preview.removeAttribute("src");
        preview.hidden = true;
        video.hidden = false;
        capture.hidden = false;
        capture.disabled = true;
        retake.hidden = true;
        retry.hidden = true;
        next.disabled = true;
        message("Opening your camera…");
        try {
            if (!window.isSecureContext) {
                throw new Error("Camera access needs a secure connection. Open this page over HTTPS or localhost.");
            }
            if (!navigator.mediaDevices?.getUserMedia) {
                throw new Error("This browser doesn't support camera access. Try a browser with camera support.");
            }
            const acquired = await navigator.mediaDevices.getUserMedia({
                video: { facingMode: { ideal: "user" }, width: { ideal: 1280 }, height: { ideal: 960 } },
                audio: false,
            });
            if (!active) {
                acquired.getTracks().forEach((track) => track.stop());
                return;
            }
            stream = acquired;
            video.srcObject = stream;
            await video.play();
            if (!active) { stopCamera(); return; }
            capture.disabled = false;
            message("Position your face in the preview, then tap Take selfie.");
        } catch (error) {
            stopCamera();
            let text = error.message || "We couldn't open your camera. Please try again.";
            if (error.name === "NotAllowedError" || error.name === "SecurityError") {
                text = "Camera permission is needed. Allow camera access in your browser settings, then retry.";
            } else if (error.name === "NotFoundError") {
                text = "No camera was found. Connect a camera or use a device with a camera, then retry.";
            } else if (error.name === "NotReadableError" || error.name === "AbortError") {
                text = "Your camera couldn't start. Close other apps using it, then retry.";
            }
            message(text, true);
            retry.hidden = false;
        } finally {
            opening = false;
        }
    }

    capture.addEventListener("click", () => {
        if (!stream || !video.videoWidth || !video.videoHeight) {
            message("Wait for the camera preview, then try again.", true);
            return;
        }
        capture.disabled = true;
        const canvas = document.createElement("canvas");
        const scale = Math.min(1, 1280 / Math.max(video.videoWidth, video.videoHeight));
        canvas.width = Math.round(video.videoWidth * scale);
        canvas.height = Math.round(video.videoHeight * scale);
        const context = canvas.getContext("2d");
        if (!context) {
            message("We couldn't capture a photo in this browser. Please retry.", true);
            capture.disabled = false;
            return;
        }
        // Save the natural camera image; only the live preview is mirrored.
        context.drawImage(video, 0, 0, canvas.width, canvas.height);
        canvas.toBlob((blob) => {
            if (!active) return;
            if (!blob) {
                message("We couldn't capture your selfie. Please try again.", true);
                capture.disabled = false;
                return;
            }
            photo = blob;
            photoURL = URL.createObjectURL(blob);
            preview.src = photoURL;
            preview.hidden = false;
            video.hidden = true;
            capture.hidden = true;
            retake.hidden = false;
            next.disabled = false;
            stopCamera();
            message("Check your selfie. Retake it or continue to verify your phone.");
        }, "image/jpeg", 0.9);
    });

    retake.addEventListener("click", startCamera);
    retry.addEventListener("click", startCamera);
    form.addEventListener("submit", async (event) => {
        event.preventDefault();
        if (!photo || submitting) return;
        submitting = true;
        next.disabled = true;
        retake.disabled = true;
        next.textContent = "Saving selfie…";
        message("Saving your selfie…");
        stopCamera();
        const data = new FormData(form);
        data.append("profile_picture", photo, "selfie.jpg");
        try {
            const response = await fetch(form.action, {
                method: "POST", body: data, credentials: "same-origin",
                headers: { Accept: "application/json" },
            });
            if (response.redirected) {
                window.location.assign(response.url);
                return;
            }
            const result = await response.json();
            if (!response.ok || !result.next) {
                throw new Error(result.error || "We couldn't save your selfie. Please try again.");
            }
            window.location.assign(result.next);
        } catch (error) {
            message(error.message || "We couldn't save your selfie. Please try again.", true);
            submitting = false;
            next.disabled = false;
            retake.disabled = false;
            next.textContent = "Continue to phone verification";
        }
    });

    window.addEventListener("pagehide", () => { active = false; stopCamera(); });
    window.addEventListener("pageshow", (event) => {
        if (event.persisted) {
            window.location.reload();
        }
    });
    startCamera();
});
