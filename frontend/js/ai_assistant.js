document.addEventListener("DOMContentLoaded", () => {
    const imageButton = document.getElementById("add-image");
    const imageInput = document.getElementById("image");
    const imageStatus = document.getElementById("image-status");
    const imagePreview = document.getElementById("image-preview");
    const imageWrap = document.getElementById("image-preview-wrap");
    const removeImage = document.getElementById("remove-image");
    if (imageButton && imageInput) {
        const menu = document.getElementById("attachment-menu");
        imageButton.addEventListener("click", () => { if (menu) menu.hidden = !menu.hidden; });
        document.getElementById("upload-photo")?.addEventListener("click", () => imageInput.click());
        document.getElementById("take-photo")?.addEventListener("click", () => { imageInput.setAttribute("capture", "environment"); imageInput.click(); });
        document.getElementById("upload-video")?.addEventListener("click", () => document.getElementById("video")?.click());
        document.getElementById("record-video")?.addEventListener("click", () => document.getElementById("start-video")?.click());
        imageInput.addEventListener("change", () => {
            if (imageInput.files.length) { imageStatus.textContent = `${imageInput.files[0].name} attached`; imagePreview.src = URL.createObjectURL(imageInput.files[0]); imageWrap.hidden = false; }
        });
        removeImage.addEventListener("click", () => { imageInput.value = ""; imageWrap.hidden = true; imagePreview.removeAttribute("src"); imageStatus.textContent = ""; });
    }
    const thinkToggle = document.getElementById("think-toggle"), thinking = document.getElementById("thinking");
    thinkToggle?.addEventListener("click", () => { const on = thinkToggle.getAttribute("aria-pressed") !== "true"; thinkToggle.setAttribute("aria-pressed", String(on)); thinking.value = String(on); });
    const form = document.getElementById("ai-form"), submitStatus = document.getElementById("submit-status");
    form?.addEventListener("submit", () => { form.querySelector(".send-button").disabled = true; submitStatus.textContent = "Analysing your farming issue… Please wait."; });
    setupAudioRecorder();
    setupVideoRecorder();
});

function getSupportedMimeType(types) {
    if (!window.MediaRecorder) {
        return "";
    }

    for (const type of types) {
        if (MediaRecorder.isTypeSupported(type)) {
            return type;
        }
    }

    return "";
}

function placeBlobInFileInput(blob, inputID, filename) {
    const input = document.getElementById(inputID);

    if (!input) {
        console.error(`Input element not found: ${inputID}`);
        return;
    }

    let extension = "webm";

    if (blob.type.includes("mp4")) {
        extension = "mp4";
    } else if (blob.type.includes("ogg")) {
        extension = "ogg";
    }

    const file = new File(
        [blob],
        `${filename}.${extension}`,
        {
            type: blob.type,
            lastModified: Date.now()
        }
    );

    const dataTransfer = new DataTransfer();
    dataTransfer.items.add(file);

    input.files = dataTransfer.files;
}

function setupAudioRecorder() {
    const startButton = document.getElementById("start-audio");
    const stopButton = document.getElementById("stop-audio");
    const status = document.getElementById("audio-status");
    const preview = document.getElementById("audio-preview");
    const removeButton = document.getElementById("remove-audio");
    const playButton = document.getElementById("play-audio");

    if (!startButton || !stopButton || !status || !preview) {
        return;
    }

    let recorder = null;
    let stream = null;
    let chunks = [];
    let previewURL = "";

    const clearPreview = () => {
        if (previewURL) {
            URL.revokeObjectURL(previewURL);
            previewURL = "";
        }
        preview.pause();
        preview.removeAttribute("src");
        preview.load();
        preview.hidden = true;
    };

    startButton.addEventListener("click", async () => {
        if (recorder && recorder.state === "recording") {
            stopRecording();
            return;
        }
        if (!navigator.mediaDevices ||
            !navigator.mediaDevices.getUserMedia) {
            status.textContent =
                "Audio recording is not supported by this browser.";
            return;
        }

        try {
            clearPreview();
            removeButton.hidden = true;
            stream = await navigator.mediaDevices.getUserMedia({
                audio: {
                    echoCancellation: true,
                    noiseSuppression: true,
                    autoGainControl: true
                }
            });

            chunks = [];

            // Opus is compact and well suited to voice/AI uploads.
            const mimeType = getSupportedMimeType([
                "audio/webm;codecs=opus",
                "audio/ogg;codecs=opus",
                "audio/webm",
                "audio/mp4"
            ]);

            const options = mimeType
                ? { mimeType: mimeType }
                : undefined;

            recorder = options
                ? new MediaRecorder(stream, {...options, videoBitsPerSecond: 900000, audioBitsPerSecond: 64000})
                : new MediaRecorder(stream);

            recorder.addEventListener("dataavailable", (event) => {
                if (event.data && event.data.size > 0) {
                    chunks.push(event.data);
                }
            });

            recorder.addEventListener("stop", () => {
                const blob = new Blob(chunks, {
                    type: recorder.mimeType || "audio/webm"
                });

                placeBlobInFileInput(
                    blob,
                    "audio",
                    "voice-recording"
                );

                if (blob.size === 0) {
                    status.textContent = "No voice audio was captured. Please try again.";
                    releaseStream(stream);
                    stream = null;
                    return;
                }

                previewURL = URL.createObjectURL(blob);
                preview.src = previewURL;
                preview.controls = true;
                preview.volume = 1;
                preview.load();
                preview.hidden = false;
                if (playButton) playButton.hidden = false;
                preview.onloadedmetadata = () => { preview.currentTime = 0; };
                preview.onerror = () => { status.textContent = "This browser cannot play the recorded format. Try Chrome or Firefox."; };

                status.textContent =
                    "Voice recording is ready. Play it before sending.";
                removeButton.hidden = false;

                releaseStream(stream);
                stream = null;
                chunks = [];
                recorder = null;
                stopButton.hidden = true;
                stopButton.disabled = true;
            });

            recorder.addEventListener("error", (event) => {
                console.error("Audio recorder error:", event.error);

                status.textContent =
                    "An error occurred while recording audio.";

                releaseStream(stream);
                stream = null;
                recorder = null;
                startButton.classList.remove("recording");
                startButton.setAttribute("aria-label", "Start voice recording");
                stopButton.hidden = true;
                stopButton.disabled = true;
            });

            recorder.start(1000);

            startButton.disabled = false;
            startButton.classList.add("recording");
            startButton.setAttribute("aria-label", "Stop voice recording");
            stopButton.disabled = false;
            stopButton.hidden = false;
            status.textContent = "Recording voice...";
        } catch (error) {
            console.error("Microphone error:", error);

            status.textContent =
                "Microphone permission was denied or unavailable.";

            releaseStream(stream);
            stream = null;
        }
    });

    const stopRecording = () => {
        if (!recorder || recorder.state === "inactive") {
            return;
        }

        recorder.stop();

        startButton.classList.remove("recording");
        startButton.setAttribute("aria-label", "Start voice recording");
        stopButton.disabled = true;
        stopButton.hidden = true;
        status.textContent = "Preparing voice recording...";
    };

    stopButton.addEventListener("click", stopRecording);
    playButton?.addEventListener("click", async () => { try { await preview.play(); status.textContent = "Playing your voice recording."; } catch { status.textContent = "The recording cannot be played in this browser. Try Chrome or Firefox."; } });
    removeButton?.addEventListener("click", () => { document.getElementById("audio").value = ""; clearPreview(); removeButton.hidden = true; if (playButton) playButton.hidden = true; status.textContent = "Voice recording removed."; });
}

function setupVideoRecorder() {
    const startButton = document.getElementById("start-video");
    const stopButton = document.getElementById("stop-video");
    const status = document.getElementById("video-status");
    const preview = document.getElementById("video-preview");
    const removeButton = document.getElementById("remove-video");

    if (!startButton || !stopButton || !status || !preview) {
        return;
    }

    let recorder = null;
    let stream = null;
    let chunks = [];

    startButton.addEventListener("click", async () => {
        if (!navigator.mediaDevices ||
            !navigator.mediaDevices.getUserMedia) {
            status.textContent =
                "Video recording is not supported by this browser.";
            return;
        }

        try {
            stream = await navigator.mediaDevices.getUserMedia({
                video: {
                    facingMode: {
                        ideal: "environment"
                    }
                },
                audio: true
            });

            chunks = [];

            const mimeType = getSupportedMimeType([
                "video/webm;codecs=vp9,opus",
                "video/webm;codecs=vp8,opus",
                "video/webm",
                "video/mp4"
            ]);

            const options = mimeType
                ? { mimeType: mimeType }
                : undefined;

            recorder = options
                ? new MediaRecorder(stream, options)
                : new MediaRecorder(stream);

            recorder.addEventListener("dataavailable", (event) => {
                if (event.data && event.data.size > 0) {
                    chunks.push(event.data);
                }
            });

            recorder.addEventListener("stop", () => {
                const blob = new Blob(chunks, {
                    type: recorder.mimeType || "video/webm"
                });

                placeBlobInFileInput(
                    blob,
                    "video",
                    "farm-video"
                );

                preview.src = URL.createObjectURL(blob);
                preview.load();
                preview.hidden = false;

                status.textContent =
                    "Video recording is ready to submit.";
                removeButton.hidden = false;

                releaseStream(stream);
                stream = null;
                chunks = [];
            });

            recorder.addEventListener("error", (event) => {
                console.error("Video recorder error:", event.error);

                status.textContent =
                    "An error occurred while recording video.";

                releaseStream(stream);
                stream = null;
            });

            recorder.start();

            startButton.disabled = true;
            stopButton.disabled = false;
            status.textContent = "Recording video...";
        } catch (error) {
            console.error("Camera error:", error);

            status.textContent =
                "Camera or microphone permission was denied.";

            releaseStream(stream);
            stream = null;
        }
    });

    stopButton.addEventListener("click", () => {
        if (!recorder || recorder.state === "inactive") {
            return;
        }

        recorder.stop();

                startButton.disabled = false;
                stopButton.disabled = true;
                recorder = null;
        status.textContent = "Preparing video recording...";
    });
    removeButton?.addEventListener("click", () => { document.getElementById("video").value = ""; preview.hidden = true; preview.removeAttribute("src"); removeButton.hidden = true; status.textContent = ""; });
}

function releaseStream(stream) {
    if (!stream) {
        return;
    }

    stream.getTracks().forEach((track) => {
        track.stop();
    });
}
