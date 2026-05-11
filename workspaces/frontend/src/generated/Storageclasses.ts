/* eslint-disable */
/* tslint:disable */
// @ts-nocheck
/*
 * ---------------------------------------------------------------
 * ## THIS FILE WAS GENERATED VIA SWAGGER-TYPESCRIPT-API        ##
 * ##                                                           ##
 * ## AUTHOR: acacode                                           ##
 * ## SOURCE: https://github.com/acacode/swagger-typescript-api ##
 * ---------------------------------------------------------------
 */

import { ApiErrorEnvelope, ApiStorageClassListEnvelope } from './data-contracts';
import { HttpClient, RequestParams } from './http-client';

export class Storageclasses<SecurityDataType = unknown> extends HttpClient<SecurityDataType> {
  /**
   * @description Returns a list of all storage classes in the cluster. When namespaceFilter is provided, authorization checks whether the user can list storage classes in that namespace instead of requiring a cluster-wide permission.
   *
   * @tags storageclasses
   * @name ListStorageClasses
   * @summary List storage classes
   * @request GET:/storageclasses
   * @response `200` `ApiStorageClassListEnvelope` Successful storage classes response
   * @response `401` `ApiErrorEnvelope` Unauthorized
   * @response `403` `ApiErrorEnvelope` Forbidden
   * @response `422` `ApiErrorEnvelope` Unprocessable Entity. Validation error.
   * @response `500` `ApiErrorEnvelope` Internal server error
   */
  listStorageClasses = (
    query?: {
      /**
       * Namespace to use for authorization scoping
       * @example "kubeflow-user-example-com"
       */
      namespaceFilter?: string;
    },
    params: RequestParams = {},
  ) =>
    this.request<ApiStorageClassListEnvelope, ApiErrorEnvelope>({
      path: `/storageclasses`,
      method: 'GET',
      query: query,
      format: 'json',
      ...params,
    });
}
