document.addEventListener("DOMContentLoaded", () => {
    /* ============================================================
       ELEMENTS
       ============================================================ */

    const form = document.getElementById("ai-form");
    const submitStatus = document.getElementById("submit-status");

    const imageButton = document.getElementById("add-image");
    const imageInput = document.getElementById("image");
    const imagePreview = document.getElementById("image-preview");
    const imageWrap = document.getElementById("image-preview-wrap");
    const imageStatus = document.getElementById("image-status");
    const removeImage = document.getElementById("remove-image");

    const videoInput = document.getElementById("video");

    const attachmentMenu = document.getElementById("attachment-menu");
    const uploadPhoto = document.getElementById("upload-photo");
    const takePhoto = document.getElementById("take-photo");
    const uploadVideo = document.getElementById("upload-video");
    const recordVideo = document.getElementById("record-video");

    const thinkToggle = document.getElementById("think-toggle");
    const thinkingInput = document.getElementById("thinking");


    /* ============================================================
       ATTACHMENT MENU
       ============================================================ */

    if (imageButton && attachmentMenu) {
        imageButton.addEventListener("click", (event) => {
            event.stopPropagation();
            attachmentMenu.hidden = !attachmentMenu.hidden;
        });

        document.addEventListener("click", (event) => {
            if (
                !attachmentMenu.contains(event.target) &&
                event.target !== imageButton
            ) {
                attachmentMenu.hidden = true;
            }
        });
    }


    /* ============================================================
       UPLOAD PHOTO
       ============================================================ */

    uploadPhoto?.addEventListener("click", () => {
        attachmentMenu.hidden = true;

        if (!imageInput) {
            return;
        }

        imageInput.removeAttribute("capture");
        imageInput.click();
    });


    /* ============================================================
       TAKE PHOTO
       ============================================================ */

    takePhoto?.addEventListener("click", () => {
        attachmentMenu.hidden = true;

        if (!imageInput) {
            return;
        }

        imageInput.setAttribute("capture", "environment");
        imageInput.click();
    });


    /* ============================================================
       UPLOAD VIDEO
       ============================================================ */

    uploadVideo?.addEventListener("click", () => {
        attachmentMenu.hidden = true;

        if (!videoInput) {
            return;
        }

        videoInput.removeAttribute("capture");
        videoInput.click();
    });


    /* ============================================================
       RECORD VIDEO
       ============================================================ */

    recordVideo?.addEventListener("click", () => {
        attachmentMenu.hidden = true;

        if (!videoInput) {
            return;
        }

        /*
         * We do NOT have a separate video recorder anymore.
         *
         * The browser's native camera picker handles recording.
         */
        videoInput.setAttribute("capture", "environment");
        videoInput.click();
    });


    /* ============================================================
       IMAGE SELECTION
       ============================================================ */

    imageInput?.addEventListener("change", () => {
        if (!imageInput.files || imageInput.files.length === 0) {
            return;
        }

        const file = imageInput.files[0];

        if (!file.type.startsWith("image/")) {
            imageInput.value = "";

            if (imageStatus) {
                imageStatus.textContent =
                    "Please select a valid image.";
            }

            return;
        }

        if (imagePreview) {
            imagePreview.src = URL.createObjectURL(file);
        }

        if (imageStatus) {
            imageStatus.textContent =
                `${file.name} attached`;
        }

        if (imageWrap) {
            imageWrap.hidden = false;
        }
    });


    /* ============================================================
       REMOVE IMAGE
       ============================================================ */

    removeImage?.addEventListener("click", () => {
        if (imageInput) {
            imageInput.value = "";
        }

        if (imagePreview) {
            imagePreview.removeAttribute("src");
        }

        if (imageStatus) {
            imageStatus.textContent = "";
        }

        if (imageWrap) {
            imageWrap.hidden = true;
        }
    });


    /* ============================================================
       VIDEO SELECTION
       ============================================================ */

    videoInput?.addEventListener("change", () => {
        if (!videoInput.files || videoInput.files.length === 0) {
            return;
        }

        const file = videoInput.files[0];

        if (!file.type.startsWith("video/")) {
            videoInput.value = "";
            return;
        }

        showVideoAttachment(file);
    });


    /* ============================================================
       VIDEO ATTACHMENT DISPLAY
       ============================================================ */

    function showVideoAttachment(file) {
        /*
         * The old page had a complete video recorder section.
         * That has been removed.
         *
         * We simply tell the user that the video is attached.
         */

        let existing = document.getElementById(
            "video-attachment-status"
        );

        if (!existing) {
            existing = document.createElement("p");
            existing.id = "video-attachment-status";
            existing.className = "attachment-status";

            const composerFeedback =
                document.querySelector(".composer-feedback");

            if (composerFeedback) {
                composerFeedback.appendChild(existing);
            }
        }

        existing.textContent =
            `🎥 ${file.name} attached`;

        existing.setAttribute("aria-live", "polite");
    }


    /* ============================================================
       THINK TOGGLE
       ============================================================ */

    thinkToggle?.addEventListener("click", () => {
        const currentlyPressed =
            thinkToggle.getAttribute("aria-pressed") === "true";

        const enabled = !currentlyPressed;

        thinkToggle.setAttribute(
            "aria-pressed",
            String(enabled)
        );

        if (thinkingInput) {
            thinkingInput.value = String(enabled);
        }
    });


    /* ============================================================
       AUDIO RECORDER
       ============================================================ */

    setupAudioRecorder();


    /* ============================================================
       FORM SUBMISSION
       ============================================================ */

    form?.addEventListener("submit", () => {
        const sendButton =
            form.querySelector(".send-button");

        if (sendButton) {
            sendButton.disabled = true;
        }

        if (submitStatus) {
            submitStatus.textContent =
                "Analysing your farming issue… Please wait.";
        }
    });
});


/* ================================================================
   AUDIO RECORDER
   ================================================================ */

function setupAudioRecorder() {
    const startButton =
        document.getElementById("start-audio");

    const stopButton =
        document.getElementById("stop-audio");

    const status =
        document.getElementById("audio-status");

    const preview =
        document.getElementById("audio-preview");

    const removeButton =
        document.getElementById("remove-audio");

    const playButton =
        document.getElementById("play-audio");

    const audioInput =
        document.getElementById("audio");


    if (
        !startButton ||
        !stopButton ||
        !status ||
        !preview
    ) {
        return;
    }


    let recorder = null;
    let stream = null;
    let chunks = [];
    let previewURL = "";


    /* ============================================================
       SUPPORTED AUDIO FORMAT
       ============================================================ */

    function getSupportedAudioMimeType() {
        if (!window.MediaRecorder) {
            return "";
        }

        const types = [
            "audio/webm;codecs=opus",
            "audio/ogg;codecs=opus",
            "audio/webm",
            "audio/mp4"
        ];

        for (const type of types) {
            if (MediaRecorder.isTypeSupported(type)) {
                return type;
            }
        }

        return "";
    }


    /* ============================================================
       RELEASE MICROPHONE
       ============================================================ */

    function releaseStream() {
        if (!stream) {
            return;
        }

        stream.getTracks().forEach((track) => {
            track.stop();
        });

        stream = null;
    }


    /* ============================================================
       CLEAR AUDIO PREVIEW
       ============================================================ */

    function clearPreview() {
        if (previewURL) {
            URL.revokeObjectURL(previewURL);
            previewURL = "";
        }

        preview.pause();
        preview.removeAttribute("src");
        preview.load();

        preview.hidden = true;

        if (playButton) {
            playButton.hidden = true;
        }
    }


    /* ============================================================
       PUT RECORDING INTO FILE INPUT
       ============================================================ */

    function putAudioIntoInput(blob) {
        if (!audioInput) {
            return;
        }

        let extension = "webm";

        if (blob.type.includes("ogg")) {
            extension = "ogg";
        } else if (blob.type.includes("mp4")) {
            extension = "mp4";
        }

        const file = new File(
            [blob],
            `voice-recording.${extension}`,
            {
                type: blob.type,
                lastModified: Date.now()
            }
        );

        const dataTransfer =
            new DataTransfer();

        dataTransfer.items.add(file);

        audioInput.files =
            dataTransfer.files;
    }


    /* ============================================================
       START RECORDING
       ============================================================ */

    startButton.addEventListener(
        "click",
        async () => {

            if (
                recorder &&
                recorder.state === "recording"
            ) {
                stopRecording();
                return;
            }


            if (
                !navigator.mediaDevices ||
                !navigator.mediaDevices.getUserMedia ||
                !window.MediaRecorder
            ) {
                status.textContent =
                    "Audio recording is not supported by this browser.";

                return;
            }


            try {
                clearPreview();

                if (removeButton) {
                    removeButton.hidden = true;
                }


                stream =
                    await navigator.mediaDevices.getUserMedia({
                        audio: {
                            echoCancellation: true,
                            noiseSuppression: true,
                            autoGainControl: true
                        }
                    });


                chunks = [];


                const mimeType =
                    getSupportedAudioMimeType();


                recorder = mimeType
                    ? new MediaRecorder(
                        stream,
                        {
                            mimeType: mimeType
                        }
                    )
                    : new MediaRecorder(stream);


                recorder.addEventListener(
                    "dataavailable",
                    (event) => {
                        if (
                            event.data &&
                            event.data.size > 0
                        ) {
                            chunks.push(event.data);
                        }
                    }
                );


                recorder.addEventListener(
                    "stop",
                    () => {

                        const blob =
                            new Blob(
                                chunks,
                                {
                                    type:
                                        recorder.mimeType ||
                                        "audio/webm"
                                }
                            );


                        if (blob.size === 0) {
                            status.textContent =
                                "No voice audio was captured. Please try again.";

                            releaseStream();

                            recorder = null;

                            return;
                        }


                        putAudioIntoInput(blob);


                        previewURL =
                            URL.createObjectURL(blob);

                        preview.src =
                            previewURL;

                        preview.hidden = false;

                        preview.controls = true;

                        if (playButton) {
                            playButton.hidden = false;
                        }


                        if (removeButton) {
                            removeButton.hidden = false;
                        }


                        status.textContent =
                            "Voice recording is ready. Play it before sending.";


                        releaseStream();

                        chunks = [];

                        recorder = null;

                        startButton.classList.remove(
                            "recording"
                        );

                        startButton.setAttribute(
                            "aria-label",
                            "Start voice recording"
                        );

                        stopButton.hidden = true;

                        stopButton.disabled = true;
                    }
                );


                recorder.addEventListener(
                    "error",
                    (event) => {

                        console.error(
                            "Audio recorder error:",
                            event.error
                        );

                        status.textContent =
                            "An error occurred while recording audio.";

                        releaseStream();

                        recorder = null;

                        startButton.classList.remove(
                            "recording"
                        );

                        startButton.setAttribute(
                            "aria-label",
                            "Start voice recording"
                        );

                        stopButton.hidden = true;

                        stopButton.disabled = true;
                    }
                );


                recorder.start(1000);


                startButton.classList.add(
                    "recording"
                );

                startButton.setAttribute(
                    "aria-label",
                    "Stop voice recording"
                );


                stopButton.hidden = false;
                stopButton.disabled = false;


                status.textContent =
                    "Recording voice...";


            } catch (error) {

                console.error(
                    "Microphone error:",
                    error
                );

                status.textContent =
                    "Microphone permission was denied or unavailable.";

                releaseStream();
            }
        }
    );


    /* ============================================================
       STOP RECORDING
       ============================================================ */

    function stopRecording() {
        if (
            !recorder ||
            recorder.state === "inactive"
        ) {
            return;
        }

        recorder.stop();

        startButton.classList.remove(
            "recording"
        );

        startButton.setAttribute(
            "aria-label",
            "Start voice recording"
        );

        stopButton.disabled = true;
        stopButton.hidden = true;

        status.textContent =
            "Preparing voice recording...";
    }


    stopButton.addEventListener(
        "click",
        stopRecording
    );


    /* ============================================================
       PLAY RECORDING
       ============================================================ */

    playButton?.addEventListener(
        "click",
        async () => {

            try {
                await preview.play();

                status.textContent =
                    "Playing your voice recording.";

            } catch (error) {

                console.error(
                    "Audio playback error:",
                    error
                );

                status.textContent =
                    "The recording cannot be played in this browser.";
            }
        }
    );


    /* ============================================================
       REMOVE AUDIO
       ============================================================ */

    removeButton?.addEventListener(
        "click",
        () => {

            if (audioInput) {
                audioInput.value = "";
            }

            clearPreview();

            removeButton.hidden = true;

            if (playButton) {
                playButton.hidden = true;
            }

            status.textContent =
                "Voice recording removed.";
        }
    );
}
/* ============================================================
   NEW FEATURES ADDED (Voice Input + File Validation)
   ============================================================ */

document.addEventListener("DOMContentLoaded", () => {
    // Voice Input (Speech-to-Text)
    const voiceInputButton = document.getElementById("voice-input");
    const voiceStatus = document.getElementById("voice-status");
    const textArea = document.querySelector(".assistant-composer textarea");

    let recognition = null;
    let isRecording = false;

    function initSpeechRecognition() {
        if (!("webkitSpeechRecognition" in window) && !("SpeechRecognition" in window)) {
            if (voiceStatus) voiceStatus.textContent = "Speech recognition not supported in this browser.";
            return;
        }

        const SpeechRecognition = window.SpeechRecognition || window.webkitSpeechRecognition;
        recognition = new SpeechRecognition();
        recognition.continuous = false;
        recognition.interimResults = true;
        recognition.lang = "en-US";

        recognition.onstart = () => {
            isRecording = true;
            if (voiceStatus) voiceStatus.textContent = "Listening...";
            if (voiceInputButton) voiceInputButton.classList.add("recording");
        };

        recognition.onresult = (event) => {
            let transcript = "";
            for (let i = event.resultIndex; i < event.results.length; i++) {
                transcript += event.results[i][0].transcript;
            }
            if (textArea) textArea.value = transcript;
        };

        recognition.onerror = (event) => {
            if (voiceStatus) voiceStatus.textContent = "Error: " + event.error;
        };

        recognition.onend = () => {
            isRecording = false;
            if (voiceStatus) voiceStatus.textContent = "";
            if (voiceInputButton) voiceInputButton.classList.remove("recording");
        };
    }

    if (voiceInputButton && textArea) {
        initSpeechRecognition();
        voiceInputButton.addEventListener("click", () => {
            if (isRecording) {
                recognition.stop();
            } else {
                try {
                    recognition.start();
                } catch (error) {
                    console.error("Speech recognition error:", error);
                }
            }
        });
    }

    // File Validation for Image Upload
    const VALID_IMAGE_TYPES = ["image/jpeg", "image/png", "image/webp", "image/gif"];
    const MAX_IMAGE_SIZE = 5 * 1024 * 1024; // 5MB

    function validateImageFile(file) {
        if (!file) return { valid: false, error: "No file selected." };
        if (!VALID_IMAGE_TYPES.includes(file.type)) {
            return { valid: false, error: "Invalid image type. Please upload JPG, PNG, WEBP, or GIF." };
        }
        if (file.size > MAX_IMAGE_SIZE) {
            return { valid: false, error: "Image too large. Maximum size is 5MB." };
        }
        return { valid: true };
    }

    const imageInput = document.getElementById("image");
    const imageStatus = document.getElementById("image-status");

    if (imageInput) {
        imageInput.addEventListener("change", () => {
            const file = imageInput.files[0];
            const validation = validateImageFile(file);
            if (!validation.valid) {
                imageInput.value = "";
                if (imageStatus) imageStatus.textContent = validation.error;
            } else {
                if (imageStatus) imageStatus.textContent = file.name + " attached";
            }
        });
    }
});

/* ============================================================
   NEW: VOICE INPUT (Speech-to-Text with Live Transcription)
   ============================================================ */

document.addEventListener("DOMContentLoaded", () => {
    const voiceInputButton = document.getElementById("voice-input");
    const voiceStatus = document.getElementById("voice-status");
    const textArea = document.getElementById("description");

    if (!voiceInputButton || !textArea) return;

    let recognition = null;
    let isRecording = false;

    // Check if browser supports speech recognition
    if (!("webkitSpeechRecognition" in window) && !("SpeechRecognition" in window)) {
        voiceStatus.textContent = "Speech recognition is not supported in this browser. Please type instead.";
        voiceInputButton.disabled = true;
        return;
    }

    const SpeechRecognition = window.SpeechRecognition || window.webkitSpeechRecognition;
    recognition = new SpeechRecognition();
    recognition.continuous = false;
    recognition.interimResults = true;  // Shows text while speaking (interim results)
    recognition.lang = "en-US";

    // When speech recognition starts
    recognition.onstart = () => {
        isRecording = true;
        voiceStatus.textContent = "Listening... Speak now.";
        voiceInputButton.classList.add("recording");
        voiceInputButton.innerHTML = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M6 18L18 6M6 6l12 12" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round"/></svg>';
    };

    // When speech is recognized (live transcription)
    recognition.onresult = (event) => {
        let finalText = "";
        let interimText = "";

        for (let i = event.resultIndex; i < event.results.length; i++) {
            const transcript = event.results[i][0].transcript;
            if (event.results[i].isFinal) {
                finalText += transcript;
            } else {
                interimText += transcript;
            }
        }

        // Combine final + interim text and show in textarea
        textArea.value = finalText + interimText;

        // Show live status
        if (interimText) {
            voiceStatus.textContent = "Heard: " + interimText;
        } else {
            voiceStatus.textContent = "Listening... Speak now.";
        }
    };

    // When speech recognition ends
    recognition.onerror = (event) => {
        voiceStatus.textContent = "Error: " + event.error;
    };

    recognition.onend = () => {
        isRecording = false;
        voiceStatus.textContent = "";
        voiceInputButton.classList.remove("recording");
        voiceInputButton.innerHTML = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 15a4 4 0 0 0 4-4V6a4 4 0 1 0-8 0v5a4 4 0 0 0 4 4Zm-7-4a7 7 0 0 0 14 0M12 18v3m-3 0h6" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round"/></svg>';
    };

    // Click voice button to start/stop
    voiceInputButton.addEventListener("click", () => {
        if (isRecording) {
            recognition.stop();
        } else {
            try {
                recognition.start();
            } catch (error) {
                console.error("Speech recognition error:", error);
                voiceStatus.textContent = "Could not start voice input. Please try again.";
            }
        }
    });
});
