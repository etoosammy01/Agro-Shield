document.addEventListener("DOMContentLoaded", () => {
    const imageButton = document.getElementById("add-image");
    const imageInput = document.getElementById("image");
    const imageStatus = document.getElementById("image-status");
    if (imageButton && imageInput) {
        imageButton.addEventListener("click", () => imageInput.click());
        imageInput.addEventListener("change", () => {
            if (imageStatus) imageStatus.textContent = imageInput.files.length ? `${imageInput.files[0].name} attached` : "";
        });
    }
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

    if (!startButton || !stopButton || !status || !preview) {
        return;
    }

    let recorder = null;
    let stream = null;
    let chunks = [];

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
            stream = await navigator.mediaDevices.getUserMedia({
                audio: true
            });

            chunks = [];

            const mimeType = getSupportedMimeType([
                "audio/webm;codecs=opus",
                "audio/webm",
                "audio/ogg;codecs=opus",
                "audio/mp4"
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
                    type: recorder.mimeType || "audio/webm"
                });

                placeBlobInFileInput(
                    blob,
                    "audio",
                    "voice-recording"
                );

                preview.src = URL.createObjectURL(blob);
                preview.hidden = false;

                status.textContent =
                    "Voice recording is ready to submit.";

                releaseStream(stream);
                stream = null;
                chunks = [];
            });

            recorder.addEventListener("error", (event) => {
                console.error("Audio recorder error:", event.error);

                status.textContent =
                    "An error occurred while recording audio.";

                releaseStream(stream);
                stream = null;
            });

            recorder.start();

            startButton.disabled = false;
            startButton.classList.add("recording");
            startButton.setAttribute("aria-label", "Stop voice recording");
            stopButton.disabled = false;
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
        status.textContent = "Preparing voice recording...";
    };

    stopButton.addEventListener("click", stopRecording);
}

function setupVideoRecorder() {
    const startButton = document.getElementById("start-video");
    const stopButton = document.getElementById("stop-video");
    const status = document.getElementById("video-status");
    const preview = document.getElementById("video-preview");

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
                preview.hidden = false;

                status.textContent =
                    "Video recording is ready to submit.";

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
        status.textContent = "Preparing video recording...";
    });
}

function releaseStream(stream) {
    if (!stream) {
        return;
    }

    stream.getTracks().forEach((track) => {
        track.stop();
    });
}
