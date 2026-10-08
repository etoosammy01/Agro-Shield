package learning

import "backend/internal/services"

// Compile-time check.
//
// This confirms that our learning providers satisfy the
// LearningProvider interface defined by the main services package.
var _ services.LearningProvider = (*GroqLearningProvider)(nil)
var _ services.LearningProvider = (*OpenRouterLearningProvider)(nil)
var _ services.LearningProvider = (*FailoverLearningProvider)(nil)