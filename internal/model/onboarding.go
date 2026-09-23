package model /* 声明 model 包。 */

// OnboardingBundle is persisted atomically; existing products/releases are
// never replaced by an onboarding request.
type OnboardingBundle struct { /* 定义 OnboardingBundle 类型。 */
	Product      *Product                /* 执行当前语句并推进处理流程。 */
	ReuseProfile bool                    /* 执行当前语句并推进处理流程。 */
	Device       ManagedDevice           /* 执行当前语句并推进处理流程。 */
	Profile      *DeviceAccessProfile    /* 执行当前语句并推进处理流程。 */
	Release      *ProtocolRelease        /* 执行当前语句并推进处理流程。 */
	PointTable   *PointTableRelease      /* 执行当前语句并推进处理流程。 */
	Binding      *ProductProtocolBinding /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
