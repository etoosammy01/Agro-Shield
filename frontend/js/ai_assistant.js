document.addEventListener("DOMContentLoaded", () => {
    /* ============================================================
       VOICE INPUT (SPEECH TO TEXT)

       The microphone button transcribes speech into the textarea.
       It never records an audio file and never submits the form.

       Started before the early-exit below so that the voice
       controller still runs on pages that have no AI form.
       ============================================================ */

    setupVoiceInput();


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
       AUDIO RECORDER (VOICE RECORDING ATTACHMENT)

       This is a separate feature from the microphone button above.
       It records an audio FILE that the farmer can review and then
       attach to the form.

       It only runs when its own start/stop controls exist in the
       page. The AI assistant page currently has no such controls,
       so nothing happens there.
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


/* ================================================================
   VOICE INPUT — SPEECH TO TEXT

   The microphone button types for the farmer:

       speak -> transcribe -> textarea -> farmer review -> Send

   The transcript is written into the normal description textarea,
   so what reaches the backend is plain text. No audio blob is ever
   created and the form is never submitted automatically.

   Audio FILE attachments are a different feature and are handled by
   the recorder above, which only runs when its own start/stop
   controls exist in the page.

   There is exactly ONE controller for #voice-input. Do not add a
   second listener for that button.
   ================================================================ */

function setupVoiceInput() {
    const button = document.getElementById("voice-input");
    const status = document.getElementById("voice-status");

    if (!button) {
        return;
    }

    /* The transcript belongs in the composer textarea. */
    const textarea = document.getElementById("description");

    const MIC_ICON =
        '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 15a4 4 0 0 0 4-4V6a4 4 0 1 0-8 0v5a4 4 0 0 0 4 4Zm-7-4a7 7 0 0 0 14 0M12 18v3m-3 0h6" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round"/></svg>';

    const STOP_ICON =
        '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M6 18L18 6M6 6l12 12" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round"/></svg>';

    const SpeechRecognition =
        window.SpeechRecognition ||
        window.webkitSpeechRecognition;

    let recognition = null;
    let listening = false;

    /* Text already in the textarea before dictation began. */
    let baseText = "";

    /* Exactly what this controller last wrote into the textarea, so we
       can tell a farmer's manual edit apart from our own writing. */
    let written = "";

    /* Set when the browser reports an error, so onend does not claim
       success on top of the error message. */
    let failed = false;

    function setStatus(message) {
        if (status) {
            status.textContent = message;
        }
    }

    function renderListening(isListening) {
        listening = isListening;

        button.classList.toggle("recording", isListening);
        button.setAttribute("aria-pressed", String(isListening));
        button.setAttribute(
            "aria-label",
            isListening ? "Stop voice input" : "Use voice input"
        );
        button.title = isListening ? "Stop listening" : "Speak to type";
        button.innerHTML = isListening ? STOP_ICON : MIC_ICON;
    }

    /* Voice input needs both a recognizer and a place to put the text.
       When either is missing, say so and leave typing untouched. */
    if (typeof SpeechRecognition !== "function" || !textarea) {
        button.disabled = true;
        button.title = "Voice input is not supported in this browser";
        setStatus(
            "Voice input is not available in this browser. Please type your question instead."
        );
        return;
    }

    function buildRecognition() {
        const instance = new SpeechRecognition();

        /* One spoken phrase per tap. Each tap appends to whatever the
           textarea already holds, so nothing the farmer typed is lost. */
        instance.continuous = false;
        instance.interimResults = true;
        instance.lang = document.documentElement.lang || "en-US";
        instance.maxAlternatives = 1;

        instance.onstart = () => {
            renderListening(true);
            setStatus("🔴 Listening… speak now.");
        };

        instance.onresult = (event) => {
            let finalText = "";
            let interimText = "";

            for (let i = event.resultIndex; i < event.results.length; i++) {
                const result = event.results[i];
                const alternative = result[0];

                if (!alternative) {
                    continue;
                }

                if (result.isFinal) {
                    finalText += alternative.transcript;
                } else {
                    interimText += alternative.transcript;
                }
            }

            /* The farmer may have edited the box while still speaking.
               Take their text as the new base for dictation, minus any
               interim words we had written, so their edit is kept and
               the stale words are not repeated back. */
            if (written && textarea.value !== written) {
                baseText = dropTrailingInterim(
                    textarea.value,
                    interimText
                );
            }

            if (finalText) {
                baseText = joinTranscript(baseText, finalText);
            }

            /* Interim words are shown live but are not committed yet, so
               they simply disappear if recognition is cancelled. */
            written = joinTranscript(baseText, interimText);
            textarea.value = written;

            if (interimText.trim()) {
                setStatus("🔴 Listening… " + interimText.trim());
            } else {
                setStatus("🔴 Listening… speak now.");
            }
        };

        instance.onerror = (event) => {
            if (!event) {
                return;
            }

            switch (event.error) {
                case "not-allowed":
                case "service-not-allowed":
                    failed = true;
                    setStatus(
                        "Microphone permission was blocked. Allow microphone access in your browser settings, or type instead."
                    );
                    break;
                case "audio-capture":
                    failed = true;
                    setStatus(
                        "No microphone was found. Check that a microphone is connected, or type instead."
                    );
                    break;
                case "network":
                    failed = true;
                    setStatus(
                        "Voice input needs an internet connection. Please check your network or type instead."
                    );
                    break;
                case "no-speech":
                    failed = true;
                    setStatus(
                        "We did not hear anything. Tap the microphone and try again."
                    );
                    break;
                case "aborted":
                    /* The farmer stopped it on purpose. onend resets the UI. */
                    break;
                default:
                    failed = true;
                    setStatus(
                        "Voice input stopped unexpectedly. Please try again or type instead."
                    );
            }
        };

        instance.onend = () => {
            /* The browser also ends the session by itself after a pause.
               Always reset the button so the farmer can tap it again.
               Nothing is sent to the server here. */
            textarea.value = textarea.value.replace(/\s+$/, "");

            if (written) {
                textarea.dispatchEvent(
                    new Event("input", { bubbles: true })
                );
            }

            written = "";
            renderListening(false);

            if (failed) {
                return;
            }

            if (textarea.value.trim()) {
                setStatus("Transcript added. Review it, then press Send.");
            } else {
                setStatus("Nothing was transcribed. Tap the microphone and try again.");
            }
        };

        return instance;
    }

    button.addEventListener("click", () => {
        /* Second tap stops the session and keeps the current transcript. */
        if (listening && recognition) {
            setStatus("Stopping…");

            try {
                recognition.stop();
            } catch (error) {
                console.error("Could not stop speech recognition:", error);
                renderListening(false);
            }

            return;
        }

        /* A fresh recognizer per session: reusing one instance after it
           has ended is not reliable across browsers. */
        recognition = buildRecognition();

        baseText = textarea.value.replace(/\s+$/, "");
        written = textarea.value;
        failed = false;

        try {
            recognition.start();
        } catch (error) {
            console.error("Speech recognition error:", error);

            /* InvalidStateError normally means a session is still open. */
            renderListening(false);
            setStatus(
                "Voice input is already starting. Please wait a moment and try again."
            );
        }
    });

    renderListening(false);
    setStatus("");
}

/* Join dictated pieces with a single space. */
function joinTranscript(base, addition) {
    const left = (base || "").replace(/\s+$/, "");
    const right = (addition || "").replace(/^\s+/, "");

    if (!left) {
        return right;
    }

    if (!right) {
        return left;
    }

    return left + " " + right;
}

/* Remove the interim words this controller last wrote, so a farmer's
   edit during dictation is not followed by the stale interim text. */
function dropTrailingInterim(value, interimText) {
    const text = (value || "").replace(/\s+$/, "");
    const interim = (interimText || "").trim();

    if (interim && text.endsWith(interim)) {
        return text.slice(0, text.length - interim.length).replace(/\s+$/, "");
    }

    return text;
}
