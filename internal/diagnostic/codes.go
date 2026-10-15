package diagnostic

// SafeCode reports membership in the typed application diagnostic catalogue.
func SafeCode(code string) bool {
	return contains(
		"arguments_invalid attempt_limit backend_busy backend_unavailable batch_admitted cancelled "+
			"candidate_timeout capacity_exceeded cleanup_pending clock_paused clock_regressed command_failed "+
			"completed configuration_access configuration_invalid cooldown deadline_exceeded device_limit "+
			"export_cleanup_failed export_commit_uncertain export_destination_invalid export_exists "+
			"export_key_unrepresentable export_prepare_failed export_publish_failed failed interrupted "+
			"interrupted_before_start invalid_arguments invalid_key license_expired license_invalid "+
			"malformed_key nonretryable not_unlimited ok output_error page_limit preview_end probe_required "+
			"provider_error rate_limited registration_rate_limited rejected running schedule_disabled "+
			"schedule_resumed schedule_starting schedule_stopped schedule_stopping schedule_waiting "+
			"service_access_error service_account_unavailable service_binding_changed service_busy "+
			"service_changed service_config_invalid service_conflict service_definition_changed "+
			"service_disabled service_executable_invalid service_identity_unverified service_log_busy "+
			"service_log_failed service_log_invalid service_manager_unavailable service_not_installed "+
			"service_observation_failed service_operation_incomplete service_owner_unknown "+
			"service_platform_unsupported service_record_invalid service_runner_failed service_running "+
			"service_start_failed service_stop_timeout service_stopped source_body_too_large source_failed "+
			"source_http_error source_pagination_error source_parse_error source_rate_limited "+
			"source_selection_empty source_selection_invalid source_timeout source_transport_error "+
			"state_busy state_error state_invalid_input state_migration_required state_not_initialized "+
			"state_record_not_found state_schema_too_new temporary_error timeout unknown_response "+
			"ui_busy ui_handle_stale ui_candidate_unavailable ui_page_invalid ui_snapshot_failed "+
			"ui_snapshot_cleanup_failed ui_collection_changed ui_key_too_large ui_export_selection_empty "+
			"ui_work_selection_empty ui_work_recovery_required ui_report_unavailable "+
			"ui_export_outcome_unknown ui_installation_invalid ui_version_mismatch ui_protocol_invalid ui_startup_timeout "+
			"ui_startup_failed ui_read_timeout ui_child_failed ui_shutdown_timeout service_diagnostic_timeout "+
			"validation_batch_inconclusive validation_batch_required validation_batch_stopped "+
			"validation_cleanup_pending validation_interrupted validation_invalid_input "+
			"validation_observation_timeout validation_probe_failed validation_provider_error "+
			"validation_rate_limited validation_timeout validation_unattributed validation_unsupported "+
			"validator_backend_unavailable validator_busy validator_clock_regressed validator_cooldown "+
			"validator_daemon_mismatch validator_identity_mismatch validator_image_required "+
			"validator_image_unavailable validator_local_socket_required validator_not_ready "+
			"validator_orphaned_session validator_output_error validator_permission_error "+
			"validator_platform_unsupported validator_probe_required validator_protocol_error "+
			"validator_runtime_error validator_state_error validator_version_mismatch ", code,
	)
}
